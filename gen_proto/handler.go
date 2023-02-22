package gen_proto

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/visonlv/protoc-gen-vkit/logger"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
)

func GenerateHandler(gen *protogen.Plugin, handlePath string) {
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		if len(f.Services) <= 0 {
			continue
		}
		err := generateOneHandler(gen, f, handlePath)
		if err != nil {
			panic(err)
		}
	}
}

func generateOneHandler(gen *protogen.Plugin, file *protogen.File, handlePath string) error {
	modUrl := ReadModUrl(handlePath + "/..")
	filePath := handlePath
	CreateDir(filePath)
	configFile, err := os.OpenFile(filePath+"/zzconfig.go", os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		logger.Infof("read file fail:%s", err)
		return err
	}
	WriteLine(configFile, "// Code generated by protoc-gen-vkit. DO NOT EDIT.")
	WriteLine(configFile, "// versions:")
	WriteLine(configFile, fmt.Sprintf("// - protoc-gen-vkit %s", release))
	WriteLine(configFile)
	WriteLine(configFile, "package handler")

	var importBuf bytes.Buffer
	var serverListBuf bytes.Buffer
	var urlsBuf bytes.Buffer

	for _, service := range file.Services {
		removeEndStr := "Service"
		fileName := service.GoName
		if strings.HasSuffix(service.GoName, removeEndStr) {
			fileName = service.GoName[:len(service.GoName)-len(removeEndStr)]
		}
		fileName = camel2Case(fileName)

		fullFillName := fmt.Sprintf("%s/%s.go", handlePath, fileName)

		newFile := false
		if !CheckFileIsExist(fullFillName) {
			newFile = true
		}

		var fileWriter *os.File
		if newFile {
			fileWriter, err = os.OpenFile(fullFillName, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
		} else {
			fileWriter, err = os.OpenFile(fullFillName, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
		}
		if err != nil {
			logger.Infof("open file failed, err:%s", err)
			return nil
		}
		defer fileWriter.Close()

		if newFile {
			WriteLine(fileWriter, "// Code generated by protoc-gen-vkit.")
			WriteLine(fileWriter, "// versions:")
			WriteLine(fileWriter, fmt.Sprintf("// - protoc-gen-vkit %s", release))
			WriteLine(fileWriter)
			WriteLine(fileWriter, "package handler")

			str, _ := os.Getwd()
			importPath := fmt.Sprintf("%s/proto/%s", modUrl, file.GoPackageName)
			logger.Infof("importPath1:%s str:%s", importPath, str)
			if !strings.HasSuffix(str, "proto") {
				logger.Infof("importPath2:%s", importPath)
				importPath = fmt.Sprintf("%s/%s", ReadModUrl(str), file.GoPackageName)
			}

			WriteLine(fileWriter, fmt.Sprintf(`
import (
	context "context"
	pb "%s"
)`, importPath))

		}

		serviceExistMap := make(map[string]bool)
		input := bufio.NewScanner(fileWriter)
		for input.Scan() {
			lineText := input.Text()
			if strings.HasPrefix(lineText, "func ") {
				lineText = lineText[strings.Index(lineText, "*")+1:]
				lineText = lineText[:strings.Index(lineText, "(")]
				lineText = strings.ReplaceAll(lineText, " ", "")
				serviceExistMap[lineText] = true
			} else if strings.HasPrefix(lineText, "type ") {
				lineText = strings.ReplaceAll(lineText, "struct {", "")
				lineText = strings.ReplaceAll(lineText, "type", "")
				lineText = strings.ReplaceAll(lineText, " ", "")
				serviceExistMap[lineText] = true
			}
		}

		if ok := serviceExistMap[service.GoName]; !ok {
			WriteLine(fileWriter, fmt.Sprintf(`
type %s struct {
}`, service.GoName))
		}

		serverListBuf.Write([]byte(fmt.Sprintf("list = append(list, &%s{})\n\t", service.GoName)))

		sd := &serviceDesc{
			ServiceType: service.GoName,
			ServiceName: string(service.Desc.FullName()),
			Metadata:    file.Desc.Path(),
		}
		for _, method := range service.Methods {
			rule, ok := proto.GetExtension(method.Desc.Options(), annotations.E_Http).(*annotations.HttpRule)
			var methodDesc *methodDesc
			if rule != nil && ok {
				methodDesc = buildHTTPRule(method, rule)
			} else {
				path := fmt.Sprintf("/%s/%s", service.Desc.FullName(), method.Desc.Name())
				methodDesc = buildMethodDesc(method, "POST", path)
			}
			sd.Methods = append(sd.Methods, methodDesc)

			methodKey := fmt.Sprintf("%s)%s", service.GoName, methodDesc.Name)
			serviceMethodKey := service.GoName + "." + methodDesc.Name

			urlsBuf.Write([]byte(fmt.Sprintf(`{
			Method:"%s",
			Url:"%s", 
			ClientStream:%t, 
			ServerStream:%t,
		},`, serviceMethodKey, methodDesc.Path, method.Desc.IsStreamingClient(), method.Desc.IsStreamingServer())))

			if ok := serviceExistMap[methodKey]; !ok {
				if method.Desc.IsStreamingClient() && method.Desc.IsStreamingServer() {
					WriteLine(fileWriter, ReplaceList(allStreamServerFunc, "${serviceName}", service.GoName, "${methodName}", method.GoName, "${req}", methodDesc.Request, "${resp}", methodDesc.Reply, "${methodPath}", methodDesc.Path))
				} else if method.Desc.IsStreamingClient() {
					WriteLine(fileWriter, ReplaceList(clientStreamServerFunc, "${serviceName}", service.GoName, "${methodName}", method.GoName, "${req}", methodDesc.Request, "${resp}", methodDesc.Reply, "${methodPath}", methodDesc.Path))
				} else if method.Desc.IsStreamingServer() {
					WriteLine(fileWriter, ReplaceList(serverStreamServerFunc, "${serviceName}", service.GoName, "${methodName}", method.GoName, "${req}", methodDesc.Request, "${resp}", methodDesc.Reply, "${methodPath}", methodDesc.Path))
				} else {
					WriteLine(fileWriter, ReplaceList(nolmalServer, "${serviceName}", service.GoName, "${methodName}", method.GoName, "${req}", methodDesc.Request, "${resp}", methodDesc.Reply, "${methodPath}", methodDesc.Path))
				}
			}
		}
	}

	WriteLine(configFile, fmt.Sprintf(`
import (
	"git.infore-robotics.cn/service-robotics-department-2/go-infore/grpcx"
	%s
)`, importBuf.String()))

	WriteLine(configFile, fmt.Sprintf(`
func GetList() []interface{} {
	list := make([]interface{}, 0)
	%s
	return list
}`, serverListBuf.String()))

	WriteLine(configFile, fmt.Sprintf(`
func GetApiEndpoint() []*grpcx.ApiEndpoint {
	return []*grpcx.ApiEndpoint{
		%s
	}
}`, urlsBuf.String()))

	return nil
}
