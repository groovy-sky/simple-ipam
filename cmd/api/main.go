package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"

	"simple-ipam/internal/api"
	"simple-ipam/internal/ipam"
	"simple-ipam/internal/store"
)

func main() {
	dsn := os.Getenv("IPAM_MYSQL_DSN")
	if dsn == "" {
		dsn = "root:root@tcp(127.0.0.1:3306)/ipam?parseTime=true"
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Printf("warning: unable to connect to MySQL: %v", err)
	}

	repo := store.NewMySQLStore(db)
	service := ipam.NewService(repo)
	handler := api.NewHandler(service)

	log.Println("listening on :8080")
	if err := http.ListenAndServe(":8080", handler); err != nil {
		log.Fatal(err)
	}
}
