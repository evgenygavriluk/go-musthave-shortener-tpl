package main

import (
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"io"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware" //через миддлваре логирование проще
	"log"
	"flag"
	"os"
	"go.uber.org/zap"
	"time"
	"strings"
)

var flagRunAddr string
var flagShortAddr string
var sugar zap.SugaredLogger

type Repository map[string]string 

var urls Repository // хранилище ссылок

// compressWriter реализует интерфейс http.ResponseWriter и позволяет прозрачно для сервера
// сжимать передаваемые данные и выставлять правильные HTTP-заголовки
type compressWriter struct {
    w  http.ResponseWriter
    zw *gzip.Writer
}

func newCompressWriter(w http.ResponseWriter) *compressWriter {
    return &compressWriter{
        w:  w,
        zw: gzip.NewWriter(w),
    }
}

func (c *compressWriter) Header() http.Header {
    return c.w.Header()
}

func (c *compressWriter) Write(p []byte) (int, error) {
    return c.zw.Write(p)
}

func (c *compressWriter) WriteHeader(statusCode int) {
    if statusCode < 300 {
        c.w.Header().Set("Content-Encoding", "gzip")
    }
    c.w.WriteHeader(statusCode)
}

// Close закрывает gzip.Writer и досылает все данные из буфера.
func (c *compressWriter) Close() error {
    return c.zw.Close()
}

// compressReader реализует интерфейс io.ReadCloser и позволяет прозрачно для сервера
// декомпрессировать получаемые от клиента данные
type compressReader struct {
    r  io.ReadCloser
    zr *gzip.Reader
}

func newCompressReader(r io.ReadCloser) (*compressReader, error) {
    zr, err := gzip.NewReader(r)
    if err != nil {
        return nil, err
    }

    return &compressReader{
        r:  r,
        zr: zr,
    }, nil
}

func (c compressReader) Read(p []byte) (n int, err error) {
    return c.zr.Read(p)
}

func (c *compressReader) Close() error {
    if err := c.r.Close(); err != nil {
        return err
    }
    return c.zr.Close()
} 

func main() {

	urls = make(Repository)

	// создаём предустановленный регистратор zap
    logger, err := zap.NewDevelopment()
    if err != nil {
        // вызываем панику, если ошибка
        panic(err)
    }
    defer logger.Sync()

    // делаем регистратор SugaredLogger
    sugar = *logger.Sugar()

	parseFlags()


	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Post("/", handlerPost)
	r.Post("/api/shorten", handlerRest)
	r.Get("/{link}", handlerGet)

	

	sugar.Infow("Starting server","addr", flagRunAddr)

	if err := http.ListenAndServe(flagRunAddr, gzipMiddleware(r)); err!=nil{
		sugar.Fatalw(err.Error(), "event", "start server")
	}
}

func gzipMiddleware(h http.Handler) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // по умолчанию устанавливаем оригинальный http.ResponseWriter как тот,
        // который будем передавать следующей функции
        ow := w

        // проверяем, что клиент умеет получать от сервера сжатые данные в формате gzip
        acceptEncoding := r.Header.Get("Accept-Encoding")
        supportsGzip := strings.Contains(acceptEncoding, "gzip")
        if supportsGzip {
            // оборачиваем оригинальный http.ResponseWriter новым с поддержкой сжатия
            cw := newCompressWriter(w)
            // меняем оригинальный http.ResponseWriter на новый
            ow = cw
            // не забываем отправить клиенту все сжатые данные после завершения middleware
            defer cw.Close()
        }

        // проверяем, что клиент отправил серверу сжатые данные в формате gzip
        contentEncoding := r.Header.Get("Content-Encoding")
        sendsGzip := strings.Contains(contentEncoding, "gzip")
        if sendsGzip {
            // оборачиваем тело запроса в io.Reader с поддержкой декомпрессии
            cr, err := newCompressReader(r.Body)
            if err != nil {
                w.WriteHeader(http.StatusInternalServerError)
                return
            }
            // меняем тело запроса на новое
            r.Body = cr
            defer cr.Close()
        }

        // передаём управление хендлеру
        h.ServeHTTP(ow, r)
    }
}

// Добавление короткой сслки в репозиторий
func (r Repository) URLtoRepository(url string, shortURL string) error{
	r[shortURL] = url
	return nil
}

// Получение исходной ссылки из репозитория по короткой
func (r Repository) URLfromRepository(shortURL string) (string, bool){
	if value, ok := r[shortURL]; ok {
		return value, true
	} 
	return "", false

}

// Обработка флагов командной строки
func parseFlags() {
    // регистрируем переменную flagRunAddr 
    // как аргумент -a со значением :8080 по умолчанию
    flag.StringVar(&flagRunAddr, "a", "localhost:8080", "address and port to run server")
	flag.StringVar(&flagShortAddr, "b", "http://localhost:8080", "base address and port to short URL")
    // парсим переданные серверу аргументы в зарегистрированные переменные
    flag.Parse()

	log.Printf("flagRunAddr = %s flagShortAddr = %s\n", flagRunAddr, flagShortAddr)

	if flagRunAddr!="localhost:8080" {
		flagShortAddr = "http://"+flagRunAddr
		log.Printf("flagShortAddr = %s\n", flagShortAddr)
	}

	// Проверяем переменные окружения
	// Если SERVER_ADDRESS установлена, то ставим ее значение, как приоритетное
	if envRunAddr := os.Getenv("SERVER_ADDRESS"); envRunAddr != "" {
        flagRunAddr = envRunAddr
    }
	// Если BASE_URL установлена, то ставим ее значение, как приоритетное
	if envBaseAddr := os.Getenv("BASE_URL"); envBaseAddr != "" {
        flagShortAddr = envBaseAddr
    }
} 


// Обрабатывает POST-запрос. Возвращает заголовок со статусом 201, если результат Ок
func handlerPost(rw http.ResponseWriter, rq *http.Request) {

	start :=time.Now()
		
	// Проверяем, есть ли в теле запроса данные (ссылка)
	body, err := io.ReadAll(rq.Body)

	if err != nil {
    	log.Fatal(err)
	}

	if string(body) != "" {
		// Сокращаем принятую ссылку
		res, err := encodeURL(string(body))
		

		// Если ошибки нет, возвращаем результат сокращения в заголовке
		// а также сохраняем короткую ссылку в хранилище

		if err == nil {
			urls.URLtoRepository(string(body), res)
			rw.Header().Set("Content-Type", "text/plain")
			rw.WriteHeader(201)
			rw.Write([]byte(flagShortAddr + "/" +res)) // flagShortAddr = http://localhost:8080/
			sugar.Infoln("Status code 201, Content Length", len(flagShortAddr + "/" +res),) 
		} else {
			rw.Write([]byte("Something wrong in encoding"))
		}

	} else {
		rw.WriteHeader(400)
		rw.Write([]byte("No URL in request"))
		sugar.Infoln("Status code 400")
	}

	duration := time.Since(start)
	sugar.Infoln("Uri", rq.RequestURI,"Method", rq.Method, "Time", duration, "Long Link", string(body)) 
}


// Обрабатывает REST-запрос. Возвращает заголовок со статусом 201, если результат Ок
func handlerRest(rw http.ResponseWriter, rq *http.Request) {

	type RequestData struct {
		URL string `json:"url"`
	}

	var inData RequestData

	type ResponseData struct {
		Result string `json:"result"`
	}

	var outData ResponseData

	start :=time.Now()

	// Проверяем, есть ли в теле запроса данные (ссылка)
	body, err := io.ReadAll(rq.Body)

	if err != nil {
    	log.Fatal(err)
	}

	if string(body) != "" {
		// Десериализуем данные из входящего JSON-запроса
		

	if err = json.Unmarshal(body, &inData); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	
		// Сокращаем принятую ссылку
		res, err:=encodeURL(inData.URL) // flagShortAddr = http://localhost:8080/

		outData.Result = flagShortAddr + "/" + res
		sugar.Infoln("inData.Url = ", inData.URL)
		sugar.Infoln("res = ", res)

		// Если ошибки нет, возвращаем результат сокращения в заголовке в JSON-формате
		// а также сохраняем короткую ссылку в хранилище

		if err == nil {
			urls.URLtoRepository(inData.URL, res) // Сохраняем данные в репозитории
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(201)
			// Сериализуем сокращенную ссылку в JSON-формат
			resp, _ := json.MarshalIndent(outData, "", " ")
			rw.Write([]byte(resp))
			sugar.Infoln("Status code 201, Content Length", len(resp)) 
		} else {
			rw.Write([]byte("Something wrong in encoding"))
		}

	} else {
		rw.WriteHeader(400)
		rw.Write([]byte("No URL in request"))
		sugar.Infoln("Status code 400")
	}

	duration := time.Since(start)
	sugar.Infoln("Uri", rq.RequestURI,"Method", rq.Method, "Time", duration, "Long Link", string(body)) 
}


func handlerGet(rw http.ResponseWriter, rq *http.Request) {
	start :=time.Now()
	// Получаем короткий URL из запроса
	shortURL := rq.URL.String()[1:]
	fmt.Println("urls = ", urls)

	// Если URL уже имеется в хранилище, возвращем в браузер ответ и делаем редирект
	if value, ok := urls.URLfromRepository(shortURL); ok {
		rw.Header().Set("Location", value)
		rw.WriteHeader(307)
	} else {
		rw.Header().Set("Location", "URL not found")
		rw.WriteHeader(400)
	}
	duration := time.Since(start)
	sugar.Infoln("Uri", rq.RequestURI,"Method", rq.Method, "Time", duration, "Short Link", shortURL) 

}

// Функция кодирования URL в сокращенный вид
func encodeURL(url string) (string, error) {
	if url != "" {
		var shortURL string
		// кодируем URL по алгоритму base64 и сокращаем строку до 6 символов
		fmt.Println("Закодированная ссылка =", base64.StdEncoding.EncodeToString([]byte(url)))
		start := len(base64.StdEncoding.EncodeToString([]byte(url)))
		shortURL = base64.StdEncoding.EncodeToString([]byte(url))[start-6:]
		fmt.Println("Короткая ссылка =", shortURL)
		return shortURL, nil
	} else {
		return "", errors.New("URL is empty")
	}
}

// Функция декодирования URL в адрес полной длины
func decodeURL(shortURL string) (string, error) {
	// Ищем shortUrl в хранилище как ключ, если есть, позвращаем значение из мапы по данному ключу
	if value, ok := urls[shortURL]; ok {
		return value, nil
	}
	return "", errors.New("short URL not foud")
}