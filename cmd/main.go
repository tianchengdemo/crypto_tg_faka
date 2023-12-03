package main

import (
	"flag"
	"fmt"
	"github.com/gin-gonic/gin"
	"gopay/internal/exts/db"
	"gopay/internal/models"
	"gopay/internal/router"
	"gopay/internal/utils/schedule"
)

func main() {
	gin.SetMode(gin.ReleaseMode)

	r := router.SetupRoutes()

	if err := db.DB.AutoMigrate(
		&models.Order{},
		&models.Transfer{},
		&models.Wallet{},
		&models.User{},
		&models.Product{},
		&models.ProductItem{},
	); err != nil {
		panic(err)
	}

	go router.RunTgBot()
	schedule.StartSchedule()

	port := flag.Int("port", 8082, "Port on which the server will run")
	flag.Parse()
	host := fmt.Sprintf("127.0.0.1:%d", *port)
	fmt.Println("运行在 " + host)

	if err := r.Run(host); err != nil {
		panic(err)
	}
}
