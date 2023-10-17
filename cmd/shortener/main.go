package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"io"
	"github.com/go-chi/chi/v5"
	"log"
	"flag"
)

var urls map[string]string // хранилище ссылок
var flagRunAddr string
var flagShortAddr string

func main() {

	parseFlags()


	urls = make(map[string]string)

	r := chi.NewRouter()
	r.Post("/", handlerPost)
	r.Get("/{link}", handlerGet)

	

	fmt.Println("Server is starter")

	log.Fatal(http.ListenAndServe(flagRunAddr, r))

}

func parseFlags() {
    // регистрируем переменную flagRunAddr 
    // как аргумент -a со значением :8080 по умолчанию
    flag.StringVar(&flagRunAddr, "a", "localhost:8080", "address and port to run server")
	flag.StringVar(&flagShortAddr, "b", "http://localhost:8080", "base address and port to short URL")
    // парсим переданные серверу аргументы в зарегистрированные переменные
    flag.Parse()

	fmt.Printf("flagRunAddr = %s", flagRunAddr)
	fmt.Printf("flagShortAddr = %s", flagShortAddr)

	if string(flagShortAddr)=="http://localhost:8080" {
		flagShortAddr = flagRunAddr
		fmt.Printf("flagShortAddr = %s", flagShortAddr)
	}
} 


// Обрабатывает POST-запрос. Возвращает заголовок со статусом 201, если результат Ок
func handlerPost(rw http.ResponseWriter, rq *http.Request) {
	fmt.Println("Отрабатывает метод", rq.Method)
	// Проверяем, есть ли в теле запроса данные (ссылка)
	body, _ := io.ReadAll(rq.Body)

	if string(body) != "" {
		// Сокращаем принятую ссылку
		res, err := encodeURL(string(body))

		// Если ошибки нет, возвращаем результат сокращения в заголовке
		// а также сохраняем короткую ссылку в хранилище

		if err == nil {
			urls[res] = string(body)
			rw.Header().Set("Content-Type", "text/plain")
			rw.WriteHeader(201)
			rw.Write([]byte(flagShortAddr + "/" +res)) // flagShortAddr = http://localhost:8080/
		} else {
			panic("Something wrong in encoding")
		}

	} else {
		rw.WriteHeader(400)
		rw.Write([]byte("No URL in request"))
	}
}

func handlerGet(rw http.ResponseWriter, rq *http.Request) {
	fmt.Println("Отрабатывает метод", rq.Method)
	// Получаем короткий URL из запроса
	shortURL := rq.URL.String()[1:]
	fmt.Println(urls)

	// Если URL уже имеется в хранилище, возвращем в браузер ответ и делаем редирект
	if value, ok := urls[shortURL]; ok {
		rw.Header().Set("Location", value)
		rw.WriteHeader(307)
	} else {
		rw.Header().Set("Location", "URL not found")
		rw.WriteHeader(400)
	}

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
