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

type gzipWriter struct {
	http.ResponseWriter
	Writer io.Writer
}

func (w gzipWriter) Write(b []byte) (int, error) {
	// w.Writer будет отвечать за gzip-сжатие, поэтому пишем в него
	return w.Writer.Write(b)
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

	if err := http.ListenAndServe(flagRunAddr, gzipHandle(r)); err!=nil{
		sugar.Fatalw(err.Error(), "event", "start server")
	}
}

func gzipHandle(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // проверяем, что клиент поддерживает gzip-сжатие
        // это упрощённый пример. В реальном приложении следует проверять все
        // значения r.Header.Values("Accept-Encoding") и разбирать строку
        // на составные части, чтобы избежать неожиданных результатов
        if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
            // если gzip не поддерживается, передаём управление
            // дальше без изменений
            next.ServeHTTP(w, r)
            return
        }

        // создаём gzip.Writer поверх текущего w
        gz, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
        if err != nil {
            io.WriteString(w, err.Error())
            return
        }
        defer gz.Close()

        w.Header().Set("Content-Encoding", "gzip")
        // передаём обработчику страницы переменную типа gzipWriter для вывода данных
        next.ServeHTTP(gzipWriter{ResponseWriter: w, Writer: gz}, r)
    })
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