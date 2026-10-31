package main

import (
	"context"
	"go-starter/internal/database"
	"log"
	"time"
)

func main() {
	db, err := database.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := database.Migrate(ctx, db); err != nil {
		log.Fatal("database migration failed")
	}
	log.Print("Launchboard database ready")
}
