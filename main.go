package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/disintegration/imaging"
)

const (
	// 文件存储根目录
	StorageRoot = "./data/"
	// 缓存文件存储根目录
	StorageCacheRoot = "./data/cache/"
	// 监听端口
	Port = ":8080"

	Slash = "/"

	PIC_CACHE_EXPIRE_TIME = 24 * time.Hour
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

	//定期清理过期缓存
	go startCleaner()

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
		getResizePicture(writer, request, filePath, width, height)
		return
	}

	getOriginalFile(writer, request, filePath)
}

func getOriginalFile(writer http.ResponseWriter, request *http.Request, filePath string) {
	if _, err := os.Stat(filePath); err == nil {
		log.Printf("orignal file exist, direct return")
		http.ServeFile(writer, request, filePath)
		return
	}

	//file, err := os.Open(filePath)
	//if err != nil {
	//	log.Printf("open file error: %v", err)
	//	http.Error(writer, "internal server error", http.StatusInternalServerError)
	//	return
	//}

	//defer file.Close()

	// 另外一种判断文件类型的方法 不需要读取文件，直接看名字
	//ext := filepath.Ext(path) // 获取 ".jpg"
	//ctype := mime.TypeByExtension(ext)
	//if ctype == "" {
	//  ctype = "application/octet-stream"
	//}
	//w.Header().Set("Content-Type", ctype)

	//2、判断文件类型
	//buffer := make([]byte, 512)
	//n, err := file.Read(buffer)
	//if err != nil {
	//	log.Printf("read file buffer fail: %v", err)
	//	http.Error(writer, "internal server error", http.StatusInternalServerError)
	//	return
	//}
	////设置Content-Type
	//contentType := http.DetectContentType(buffer[:n])
	//if contentType != "" {
	//	writer.Header().Set("Content-Type", contentType)
	//}
	//
	//// 重置文件指针，Seek(0, 0) 表示回到文件开头
	//_, err = file.Seek(0, 0)
	//if err != nil {
	//	log.Printf("read file buffer fail: %v", err)
	//	http.Error(writer, "internal server error", http.StatusInternalServerError)
	//	return
	//}
	//
	//// 3、流式发送
	//_, err = io.Copy(writer, file)
	//if err != nil {
	//	log.Printf("copy file error: %v", err)
	//	http.Error(writer, "internal server error", http.StatusInternalServerError)
	//	return
	//}
	//log.Printf("✅ get file request: %v", file.Name())
}

func defaultHandler(writer http.ResponseWriter, request *http.Request) {
	http.Error(writer, "unsupported request", http.StatusBadRequest)
}

func getResizePicture(writer http.ResponseWriter, request *http.Request, filePath string, widthStr string, heightStr string) {
	width, _ := strconv.Atoi(widthStr)
	height, _ := strconv.Atoi(heightStr)

	if width <= 0 || height <= 0 {
		getOriginalFile(writer, request, filePath)
		return
	}

	tryGetFromCache(writer, request, filePath, width, height)
	log.Printf("get resizePicture: pic -> width, height :%v -> %v, %v", filePath, width, height)
}

func tryGetFromCache(writer http.ResponseWriter, request *http.Request, filePath string, width, height int) {
	//aaa_1x10.png
	newFileName, suffix := genFileName(filePath, width, height)
	sum := md5.Sum([]byte(newFileName))
	encodeFileName := StorageCacheRoot + hex.EncodeToString(sum[:]) + "." + suffix

	//若存在缓存，直接返回
	if _, err := os.Stat(encodeFileName); err == nil {
		log.Printf("命中图片缓存: %s", encodeFileName)
		http.ServeFile(writer, request, encodeFileName)
		return
	}

	//若不存在缓存，则打开原图片，并进行缩放
	srcImage, err := imaging.Open(filePath)
	//如果打不开原图片，返回原文件
	if err != nil {
		log.Printf("open image fail: %v", err)
		getOriginalFile(writer, request, filePath)
		return
	}

	var processedImg image.Image
	// ⭐️ 核心操作：缩放
	// imaging.Thumbnail: 保持比例缩放并裁剪，适合做头像/列表图
	// imaging.Resize: 强制拉伸或保持比例
	// 这里我们用 Fit (保持比例，适应框框)
	processedImg = imaging.Fit(srcImage, width, height, imaging.Lanczos)

	//缩放后进行缓存
	file, err := os.Create(encodeFileName)
	if err != nil {
		log.Printf("create cache file error: %v", err)
		return
	}
	err = png.Encode(file, processedImg)
	if err != nil {
		log.Printf("图片缓存失败:%s", err)
		return
	}

	http.ServeFile(writer, request, encodeFileName)
	log.Printf("未命中图片缓存:%s", encodeFileName)
}

func genFileName(filePath string, width int, height int) (string, string) {
	//abc.png
	fullName := filePath[strings.LastIndex(filePath, "/")+1:]
	fullNameArray := strings.Split(fullName, ".")
	//abc
	fileName := fmt.Sprintf("%s_%dx%d", fullNameArray[0], width, height)
	//png
	suffix := fullNameArray[1]
	return fileName, suffix
}

func startCleaner() {
	ticker := time.NewTicker(1 * time.Hour)
	for range ticker.C {
		log.Printf("begin clean")
		cleanExpiredFiles()
		log.Printf("end clean")
	}
}

func cleanExpiredFiles() {
	err := filepath.Walk(StorageCacheRoot, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if time.Since(info.ModTime()) > PIC_CACHE_EXPIRE_TIME {
			log.Printf("delete expired file: %s\n", path)
			os.Remove(path)
		}
		return nil
	})
	if err != nil {
		log.Printf("clean error: %v", err)
	}
}
