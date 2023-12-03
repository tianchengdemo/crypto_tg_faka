package router

import (
	"fmt"
	"github.com/gin-gonic/gin"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"gopay/internal/exts/cache"
	"gopay/internal/exts/config"
	"gopay/internal/exts/db"
	"gopay/internal/exts/tg_bot"
	"gopay/internal/handlers/admin_handler"
	"gopay/internal/handlers/tg_handler"
	"gopay/internal/models"
	"gopay/internal/router/middleware"
	"gopay/internal/utils/handle_defender"
	"strings"
)

func SetupRoutes() *gin.Engine {

	config.LoadAllConfig()
	db.InitAllDB()
	tg_bot.InitTGBot()
	cache.InitCache()

	r := gin.Default()

	r.GET("/api/admin/token_login", admin_handler.TokenLogin)
	r.POST("/api/admin/logout", admin_handler.Logout)

	r.POST("/api/admin/info", middleware.AdminAuthMiddleware(), admin_handler.Info)
	r.POST("/api/admin/dashboard", middleware.AdminAuthMiddleware(), admin_handler.Dashboard)
	r.POST("/api/admin/dashboard_chart", middleware.AdminAuthMiddleware(), admin_handler.DashboardChart)

	r.POST("/api/admin/product", middleware.AdminAuthMiddleware(), admin_handler.FetchList[*models.Product])
	r.POST("/api/admin/create_product", middleware.AdminAuthMiddleware(), admin_handler.CreateProduct)
	r.POST("/api/admin/edit_product", middleware.AdminAuthMiddleware(), admin_handler.EditProduct)
	r.POST("/api/admin/delete_products", middleware.AdminAuthMiddleware(), admin_handler.DeleteEntities[*models.Product])

	r.POST("/api/admin/product_item", middleware.AdminAuthMiddleware(), admin_handler.FetchList[*models.ProductItem])
	r.POST("/api/admin/create_product_items", middleware.AdminAuthMiddleware(), admin_handler.CreateProductItems)
	r.POST("/api/admin/delete_product_items", middleware.AdminAuthMiddleware(), admin_handler.DeleteProductItems) //这个要更新product库存，deletebefore是按删除个数执行的，效率低
	//r.POST("/api/admin/delete_product_items", middleware.AdminAuthMiddleware(), admin_handler.DeleteEntities[*models.ProductItem])

	r.POST("/api/admin/order", middleware.AdminAuthMiddleware(), admin_handler.FetchList[*models.Order])
	r.POST("/api/admin/release_orders", middleware.AdminAuthMiddleware(), admin_handler.ReleaseOrders)

	r.POST("/api/admin/transfer", middleware.AdminAuthMiddleware(), admin_handler.FetchList[*models.Transfer])

	r.POST("/api/admin/wallet", middleware.AdminAuthMiddleware(), admin_handler.FetchList[*models.Wallet])
	r.POST("/api/admin/generate_wallet", middleware.AdminAuthMiddleware(), admin_handler.GenerateWallet)
	r.POST("/api/admin/import_wallet", middleware.AdminAuthMiddleware(), admin_handler.ImportWallet)
	r.POST("/api/admin/edit_wallet", middleware.AdminAuthMiddleware(), admin_handler.EditWallet)
	r.POST("/api/admin/delete_wallets", middleware.AdminAuthMiddleware(), admin_handler.DeleteEntities[*models.Wallet])
	r.POST("/api/admin/delete_all_wallets", middleware.AdminAuthMiddleware(), admin_handler.DeleteAllEntities[*models.Wallet])
	r.POST("/api/admin/refresh_wallet", middleware.AdminAuthMiddleware(), admin_handler.RefreshWallet)
	r.GET("/api/admin/export_wallets", middleware.AdminAuthMiddleware(), admin_handler.ExportWallets)

	r.POST("/api/admin/setting", middleware.AdminAuthMiddleware(), admin_handler.Setting)
	r.POST("/api/admin/edit_setting", middleware.AdminAuthMiddleware(), admin_handler.EditSetting)

	return r
}

func RunTgBot() {
	bot := tg_bot.Bot
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		go handleUpdate(update)
	}
}

func handleUpdate(update tgbotapi.Update) {
	defer func() {
		if r := recover(); r != nil {
			msgText := fmt.Sprintf("机器人处理消息崩溃, Error: %v", r)
			handle_defender.HandlePanic(r, msgText)
		}
	}()

	if update.Message != nil {
		if update.Message.IsCommand() {
			switch update.Message.Command() {
			case "start":
				tg_handler.StartCommand(update)
			case "login":
				tg_handler.LoginCommand(update)
			case "product_list":
				tg_handler.ProductList(update)
			case "paid_order":
				tg_handler.PaidOrder(update)
			}
		}
	}
	if update.CallbackQuery != nil {
		callbackData := update.CallbackQuery.Data
		if strings.HasPrefix(callbackData, tg_handler.ProductListPagePrefix) {
			tg_handler.ProductList(update)
		} else if strings.HasPrefix(callbackData, tg_handler.ProductDetailPrefix) {
			tg_handler.ProductDetail(update)
		} else if strings.HasPrefix(callbackData, tg_handler.PayOrderPrefix) {
			tg_handler.PayOrder(update)
		} else if strings.HasPrefix(callbackData, tg_handler.GetPaidOrderResultPrefix) {
			tg_handler.GetPaidOrderResult(update)
		} else if callbackData == "delete_msg" {
			tg_handler.CallbackDeleteMsg(update)
		}

	}
}
