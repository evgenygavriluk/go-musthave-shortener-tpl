package main

import (
    "flag"
)

// неэкспортированная переменная flagRunAddr содержит адрес и порт для запуска сервера


// parseFlags обрабатывает аргументы командной строки 
// и сохраняет их значения в соответствующих переменных
func parseFlags() {
    // регистрируем переменную flagRunAddr 
    // как аргумент -a со значением :8080 по умолчанию
    flag.StringVar(&flagRunAddr, "a", ":8080", "address and port to run server")
	flag.StringVar(&flagShortAddr, "s", "http://localhost:8080", "base address and port to short URL")
    // парсим переданные серверу аргументы в зарегистрированные переменные
    flag.Parse()
} 