package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/golang/protobuf/proto"
	pb "github.com/golang/protobuf/protoc-gen-go/plugin"
)

// Error prints an error and exits
func Error(err error, msgs ...string) {
	s := strings.Join(msgs, " ") + ":" + err.Error()
	log.Print("protoc-gen-go: error:", s)
	os.Exit(1)
}

type method struct {
	name   string
	input  string
	output string
}

type class struct {
	name    string
	methods []*method
}

type server struct {
	namespace    string
	protoPackage string
	classes      map[string]*class
}

func main() {
	temp := template.Must(template.New("grpc").Parse(classTemplate))

	data, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		Error(err, "reading input")
	}

	req := pb.CodeGeneratorRequest{}
	if err = proto.Unmarshal(data, &req); err != nil {
		Error(err, "parsing input proto")
	}

	if len(req.FileToGenerate) == 0 {
		Error(fmt.Errorf("no files"), "no files to generate")
	}

	files := []*pb.CodeGeneratorResponse_File{}
	servers := map[string]*server{}

	for i := range req.FileToGenerate {
		file := req.ProtoFile[i]
		if len(file.Service) == 0 {
			return
		}

		namespace := phpNamespace(*file.Package)
		s, ok := servers[namespace]
		if !ok {
			s = &server{
				namespace:    namespace,
				protoPackage: *file.Package,
				classes:      map[string]*class{},
			}
			servers[namespace] = s
		}

		for _, svc := range file.Service {
			c, ok := s.classes[*svc.Name]
			if !ok {
				c = &class{
					name: *svc.Name,
				}
				s.classes[*svc.Name] = c
			}
			for _, meth := range svc.Method {
				m := method{
					name:   *meth.Name,
					input:  messageType(*meth.InputType),
					output: messageType(*meth.OutputType),
				}
				c.methods = append(c.methods, &m)
			}
		}
	}

	for _, s := range servers {

		for _, c := range s.classes {

			t := &tempStruct{
				Namespace: s.namespace,
				Package:   s.protoPackage,
				Class:     c.name,
			}
			for _, m := range c.methods {
				t.Methods = append(t.Methods,
					Method{
						Name:   m.name,
						Input:  m.input,
						Output: m.output,
					},
				)
			}

			buff := &bytes.Buffer{}
			if err = temp.Execute(buff, t); err != nil {
				Error(err, "failed to execute template")
			}

			parts := strings.Split(s.namespace, `\`)
			parts = append(parts, c.name+"Server.php")
			filename := filepath.Join(parts...)
			content := buff.String()
			f := &pb.CodeGeneratorResponse_File{
				Name:    &filename,
				Content: &content,
			}
			files = append(files, f)
		}
	}

	resp := pb.CodeGeneratorResponse{
		File: files,
	}

	// Send back the results.
	data, err = proto.Marshal(&resp)
	if err != nil {
		Error(err, "failed to marshal output proto")
	}
	_, err = os.Stdout.Write(data)
	if err != nil {
		Error(err, "failed to write output proto")
	}
}

func phpNamespace(in string) string {
	parts := strings.Split(in, ".")
	for i, p := range parts {
		parts[i] = strings.Title(p)
	}
	return strings.Join(parts, `\`)
}

func messageType(in string) string {
	parts := strings.Split(in, ".")
	m := parts[len(parts)-1]
	return phpNamespace(strings.Join(parts[:len(parts)-1], `\`) + `\` + m)
}

// Method ...
type Method struct {
	Name   string
	Input  string
	Output string
}

type tempStruct struct {
	Namespace string
	Class     string
	Package   string
	Methods   []Method
}

var classTemplate = `<?php
// GENERATED CODE -- DO NOT EDIT!
namespace {{ .Namespace }};

interface {{ .Class }}Service {
{{- range .Methods }}
    public function {{ .Name }}({{ .Input }} $req) : {{ .Output }};
{{- end }}
}

class {{ .Class }}Server {
    private $routes;
    private $handler;

    function __construct(\{{ .Namespace }}\{{ .Class }}Service $implementation) {
        $this->handler = $implementation;
        $this->routes = array(
{{- range .Methods }}
            '/{{ $.Package }}.{{ $.Class }}/{{ .Name }}' => function($body) {
                $req = new {{ .Input }};
                $req->mergeFromString($body);
                $resp = $this->handler->{{ .Name }}($req);
                return $resp->serializeToString();
            },
{{ end }}
            // dummy key to ensure this is a valid array
            0 => 42
        );
    }

    // low-level handler
    function handle(string $path, string $body) : string {
        $f = $this->routes[$path] ?: null;
        if (is_null($f)) {
            throw new \Exception("unknown method", 404);
        } else {
          return $f($body);
        }
    }

    // high-level handler
    function serve() {
        if ( $_SERVER['REQUEST_METHOD'] != "POST" ) {
			http_response_code(400);
			print("invalid HTTP request method");
			exit();
		}
        try {
            $path = $_SERVER['REQUEST_URI'];
            $body = file_get_contents('php://input');
            $resp = $this->handle($path, $body);
            header('Content-Type: application/grpc+proto');
            print($resp);
         } catch (\Exception $e) {
            $code = $e->getCode();
            if ($code < 400 || $code > 600) {
                $code = 500;
            }
            http_response_code($code);
            print($e->getMessage());
        }
    }
}
`
