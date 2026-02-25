package main

import (
  "fmt"
  "image"
  "image/jpeg"
  "io"
  "log"
  "net/http"
  "os"
  "strconv"

  "github.com/disintegration/imaging"
)

const (
  // 文件存储根目录
  StorageRoot = "./data/"
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
  fileName := request.URL.Path[1:]
  //fileName := request.RequestURI
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
  log.Printf("✅ put file request: %v ,file size: %v", filePath, written)
}

func getHandler(writer http.ResponseWriter, request *http.Request, filePath string) {
  // 判断文件是否存在
  if _, err := os.Stat(filePath); os.IsNotExist(err) {
    http.Error(writer, "file not exists", http.StatusNotFound)
    return
  }

  width := request.URL.Query().Get("w")
  height := request.URL.Query().Get("h")
  //如果有 w和h参数，重置图片大小，否则获取原文件
  if width != "" && height != "" {
    getResizePicture(writer, filePath, width, height)
    return
  }

  getOriginalFile(writer, filePath)
}

func getOriginalFile(writer http.ResponseWriter, filePath string) {
  file, err := os.Open(filePath)
  if err != nil {
    log.Printf("open file error: %v", err)
    http.Error(writer, "internal server error", http.StatusInternalServerError)
    return
  }

  defer file.Close()

  // 另外一种判断文件类型的方法 不需要读取文件，直接看名字
  //ext := filepath.Ext(path) // 获取 ".jpg"
  //ctype := mime.TypeByExtension(ext)
  //if ctype == "" {
  //  ctype = "application/octet-stream"
  //}
  //w.Header().Set("Content-Type", ctype)

  //2、判断文件类型
  buffer := make([]byte, 512)
  n, err := file.Read(buffer)
  if err != nil {
    log.Printf("read file buffer fail: %v", err)
    http.Error(writer, "internal server error", http.StatusInternalServerError)
    return
  }
  //设置Content-Type
  contentType := http.DetectContentType(buffer[:n])
  if contentType != "" {
    writer.Header().Set("Content-Type", contentType)
  }

  // 重置文件指针，Seek(0, 0) 表示回到文件开头
  _, err = file.Seek(0, 0)
  if err != nil {
    log.Printf("read file buffer fail: %v", err)
    http.Error(writer, "internal server error", http.StatusInternalServerError)
    return
  }

  // 3、流式发送
  _, err = io.Copy(writer, file)
  if err != nil {
    log.Printf("copy file error: %v", err)
    http.Error(writer, "internal server error", http.StatusInternalServerError)
    return
  }
  log.Printf("✅ get file request: %v", file.Name())
}

func defaultHandler(writer http.ResponseWriter, request *http.Request) {
  http.Error(writer, "unsupported request", http.StatusBadRequest)
}

func getResizePicture(writer http.ResponseWriter, filePath string, widthStr string, heightStr string) {
  width, _ := strconv.Atoi(widthStr)
  height, _ := strconv.Atoi(heightStr)

  srcImage, err := imaging.Open(filePath)
  //如果打不开图片，返回原文件
  if err != nil {
    log.Printf("open image fail: %v", err)
    getOriginalFile(writer, filePath)
    return
  }

  var processedImg image.Image
  if width > 0 && height > 0 {
    // 3. ⭐️ 核心操作：缩放
    // imaging.Thumbnail: 保持比例缩放并裁剪，适合做头像/列表图
    // imaging.Resize: 强制拉伸或保持比例
    // 这里我们用 Fit (保持比例，适应框框)
    processedImg = imaging.Fit(srcImage, width, height, imaging.Lanczos)
  } else {
    processedImg = srcImage
  }
  writer.Header().Set("Content-Type", "jpg")
  err = jpeg.Encode(writer, processedImg, &jpeg.Options{Quality: 80})
  if err != nil {
    log.Printf("Image encode error: %v", err)
  }
  log.Printf("get resizePicture: pic -> width, height :%v -> %v, %v", filePath, width, height)
}
