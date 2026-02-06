package main

import (
  "fmt"
  "io"
  "log"
  "net/http"
  "os"
)

const (
  // 文件存储根目录
  StorageRoot = "./data"
  // 监听端口
  Port = ":8080"

  Slash = "/"
)

var httpHandlerMap map[string]func(writer http.ResponseWriter, request *http.Request)

func main() {
  // 确保目录存在
  err := os.MkdirAll(StorageRoot, 0755)
  if err != nil {
    log.Fatal("创建文件夹失败:", err)
  }

  // 注册路由处理函数
  http.HandleFunc("/", handler)

  fmt.Printf("🚀 GOSS Object Storage running on %s\n", Port)
  log.Fatal(http.ListenAndServe(Port, nil))
}

func handler(writer http.ResponseWriter, request *http.Request) {
  //fmt.Println("handler")
  //fmt.Println(request.RequestURI)

  fileName := request.RequestURI
  if fileName == Slash {
    http.Error(writer, "fileName is necessary", http.StatusBadRequest)
    return
  }

  localFile := StorageRoot + fileName

  switch request.Method {
  case http.MethodPut:
    putHandler(writer, request, localFile)
  case http.MethodGet:
    getHandler(writer, request, localFile)
  default:
    defaultHandler(writer, request)
  }
}

func putHandler(writer http.ResponseWriter, request *http.Request, filePath string) {
  //创建本地文件
  file, err := os.Create(filePath)
  if err != nil {
    log.Printf("create file error: %v", err)
    http.Error(writer, "internal server error", http.StatusInternalServerError)
    return
  }
  //流式拷贝
  // 不要用 io.ReadAll！那会把大文件全部加载到内存，瞬间 OOM。
  // io.Copy 会在两个流之间搬运数据，内存占用极小（32KB buffer）。
  written, err := io.Copy(file, request.Body)
  if err != nil {
    log.Printf("copy file error: %v", err)
    http.Error(writer, "internal server error", http.StatusInternalServerError)
    return
  }

  writer.WriteHeader(http.StatusCreated)
  fmt.Fprintf(writer, "Saved %d bytes", written)
  log.Printf("put file request: %v ,file size: %v", filePath, written)
}

func getHandler(writer http.ResponseWriter, request *http.Request, filePath string) {
  //打开文件
  file, err := os.Open(filePath)
  if err != nil {
    if os.IsNotExist(err) {
      http.Error(writer, "file not exists", http.StatusNotFound)
    } else {
      log.Printf("open file error: %v", err)
      http.Error(writer, "internal server error", http.StatusInternalServerError)
    }
    return
  }
  defer file.Close()
  // 流式发送
  _, err = io.Copy(writer, file)
  if err != nil {
    log.Printf("copy file error: %v", err)
    return
  }
  log.Printf("✅ get file request: %v", filePath)
}

func defaultHandler(writer http.ResponseWriter, request *http.Request) {
  http.Error(writer, "unsupported request", http.StatusBadRequest)
}
