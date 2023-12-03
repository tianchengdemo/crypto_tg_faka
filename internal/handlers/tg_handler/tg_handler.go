package tg_handler

import (
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"gopay/internal/exts/config"
	"gopay/internal/exts/db"
	"gopay/internal/exts/tg_bot"
	"gopay/internal/models"
	"gopay/internal/services"
	"gopay/internal/utils/functions"
	"strconv"
	"strings"
	"time"
)

// 修改消息，文本为空则忽视，markup为空则新发一个消息
var ProductListPagePrefix = "p_l_p_"
var ProductDetailPrefix = "p_d_"
var PayOrderPrefix = "p_o_"
var GetPaidOrderResultPrefix = "g_p_o_r_"

func StartCommand(update tgbotapi.Update) {
	msgText := config.WelcomeMsg(map[string]interface{}{})
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, msgText)
	tg_bot.Bot.Send(msg)
}

func LoginCommand(update tgbotapi.Update) {
	chatID := update.Message.Chat.ID
	expireTime := time.Second * 60

	if chatID != config.GetSiteConfig().AdminTGID {
		return
	}

	token := services.SetAdminLoginUrlSession(expireTime)
	loginUrl := fmt.Sprintf("%s/api/admin/token_login?token=%s", config.GetSiteConfig().Host, token)

	text := fmt.Sprintf("<a href=\"%s\">一次性登录地址(60秒内有效)</a>", loginUrl)
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, text)
	msg.ParseMode = "HTML"
	sentMsg, err := tg_bot.Bot.Send(msg)
	if err != nil {
		//log.Println("Error sending message:", err)
	}

	//清除login session
	services.ClearAdminLoginTokenSession()

	//定时删除
	go func(ChatID int64, messageID int) {
		time.Sleep(expireTime)
		tg_bot.DeleteMsg(chatID, messageID)
	}(chatID, sentMsg.MessageID)

}

func ProductList(update tgbotapi.Update) {
	currentPage := 1
	if update.CallbackQuery != nil {
		callbackData := update.CallbackQuery.Data
		value, err := strconv.Atoi(strings.TrimPrefix(callbackData, ProductListPagePrefix))
		if err == nil {
			currentPage = value
		}
	}

	pagination := services.Pagination{Limit: 10, Page: currentPage}
	err := services.GetProductsByCustomer(&pagination)
	if err != nil {
		tg_bot.Bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "获取失败"))
	}

	var products []models.Product
	for _, item := range pagination.Items {
		products = append(products, item.(models.Product))
	}

	msgText := config.ProductListMsg(map[string]interface{}{})
	rows := append(paginationToRows(pagination), deleteMsgRow())
	replyMarkup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	if update.Message != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, msgText)
		msg.ReplyMarkup = replyMarkup
		tg_bot.Bot.Send(msg)
	} else {
		msg := tgbotapi.NewEditMessageText(update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Message.MessageID, msgText)
		replyMarkup := tgbotapi.NewInlineKeyboardMarkup(rows...)
		msg.ReplyMarkup = &replyMarkup
		tg_bot.Bot.Send(msg)
	}
}

func ProductDetail(update tgbotapi.Update) {
	callbackData := update.CallbackQuery.Data
	productID, err := uuid.Parse(strings.TrimPrefix(callbackData, ProductDetailPrefix))
	if err != nil {
		callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "id错误")
		tg_bot.Bot.Request(callback)
		return
	}

	product, err := services.GetProductByIDByCustomer(productID)
	if err != nil {
		callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "商品不存在")
		tg_bot.Bot.Request(callback)
		return
	}

	msgText := config.ProductDetailMsg(map[string]interface{}{
		"Product": product,
	})
	newMsg := tgbotapi.NewEditMessageText(update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Message.MessageID, msgText)

	//backRow := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("返回", ProductListPagePrefix+"1")}
	goBackRow := GoBackRow(ProductListPagePrefix + "1")
	paymentRow := paymentSelectRow(product.ID)
	if len(paymentRow) == 0 {
		callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "没有设置支付方式")
		tg_bot.Bot.Request(callback)
		return
	}
	closeRow := deleteMsgRow()
	markupPtr := tgbotapi.NewInlineKeyboardMarkup(paymentRow, goBackRow, closeRow)
	newMsg.ReplyMarkup = &markupPtr
	tg_bot.Bot.Send(newMsg)

}

func CallbackDeleteMsg(update tgbotapi.Update) {
	chatID := update.CallbackQuery.Message.Chat.ID
	messageID := update.CallbackQuery.Message.MessageID
	tg_bot.DeleteMsg(chatID, messageID)
}

func PayOrder(update tgbotapi.Update) {
	senderChatID := update.CallbackQuery.Message.Chat.ID
	senderMsgID := update.CallbackQuery.Message.MessageID
	senderUsername := update.CallbackQuery.From.UserName

	callbackData := update.CallbackQuery.Data
	value := strings.TrimPrefix(callbackData, PayOrderPrefix)
	parts := strings.Split(value, "_")
	if len(parts) != 2 {
		callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "参数长度错误")
		tg_bot.Bot.Request(callback)
		return
	}

	productIDString := parts[0]
	paymentOptionString := parts[1]

	if !config.IsPaymentEnable(paymentOptionString) {
		callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "支付方式不存在")
		tg_bot.Bot.Request(callback)
		return
	}
	var paymentOption *config.PaymentOption
	paymentOption, err := config.ParsePaymentMethod(paymentOptionString)
	if err != nil {
		callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "支付方式错误")
		tg_bot.Bot.Request(callback)
		return
	}
	productID, err := uuid.Parse(productIDString)
	if err != nil {
		callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "商品ID错误")
		tg_bot.Bot.Request(callback)
		return
	}
	product, err := services.GetProductByIDByCustomer(productID)
	if err != nil {
		callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "商品不存在")
		tg_bot.Bot.Request(callback)
		return
	}

	// 一個用戶只能有一個訂單，由於放在前面釋放，先釋放后创建
	var toReleaseOrderIDs []uuid.UUID
	if result := db.DB.Model(&models.Order{}).Where("status = 0 and tg_chat_id = ?", senderChatID).Pluck("id", &toReleaseOrderIDs); result.RowsAffected > 0 {
		services.ReleaseOrders(toReleaseOrderIDs)
	}

	// 创建订单
	order, err := services.CreateOrder(paymentOption.Currency, string(paymentOption.Network), product, senderChatID, senderUsername)
	if err != nil {
		callback := tgbotapi.NewCallback(update.CallbackQuery.ID, err.Error())
		tg_bot.Bot.Request(callback)
		return
	}

	// 生成图片
	qrImageBytes, err := functions.GenerateQrCodeBytes(order.WalletAddress)
	if err != nil {
		callback := tgbotapi.NewCallback(update.CallbackQuery.ID, err.Error())
		tg_bot.Bot.Request(callback)
		return
	}

	photoFileBytes := tgbotapi.FileBytes{Name: "qr.png", Bytes: qrImageBytes}
	photoMsg := tgbotapi.NewPhoto(senderChatID, photoFileBytes)
	photoMsg.Caption = config.PayOrderMsg(map[string]interface{}{
		"Order": order,
	})
	photoMsg.ParseMode = "HTML"

	result, _ := tg_bot.Bot.Send(photoMsg)

	// 删除原消息
	tg_bot.DeleteMsg(senderChatID, senderMsgID)

	// 给订单设置msgID用于删除
	services.SetOrderTGMsgID(order.ID, int64(result.MessageID))

}
func PaidOrder(update tgbotapi.Update) {
	chatID := update.Message.Chat.ID

	paidOrders, err := services.GetPaidOrdersByCustomer(chatID)
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "获取订单错误")
		tg_bot.Bot.Send(msg)
		return
	}
	if len(paidOrders) == 0 {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "没有已付订单")
		tg_bot.Bot.Send(msg)
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, paidOrder := range paidOrders {
		buttonText := fmt.Sprintf("%s %s %s%s ", time.Unix(paidOrder.CreateTime, 0).Format("2006-01-02"), paidOrder.Product.Name, paidOrder.Price, paidOrder.Currency)
		row := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(buttonText, GetPaidOrderResultPrefix+paidOrder.ID.String())}
		rows = append(rows, row)
	}
	msgText := config.PaidOrderListMsg(map[string]interface{}{})
	rows = append(rows, deleteMsgRow())
	replyMarkup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, msgText)
	msg.ReplyMarkup = replyMarkup
	tg_bot.Bot.Send(msg)
}

func GetPaidOrderResult(update tgbotapi.Update) {
	callbackData := update.CallbackQuery.Data
	senderChatID := update.CallbackQuery.Message.Chat.ID
	senderMsgID := update.CallbackQuery.Message.MessageID

	orderID, err := uuid.Parse(strings.TrimPrefix(callbackData, GetPaidOrderResultPrefix))
	if err != nil {
		callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "id错误")
		tg_bot.Bot.Request(callback)
		return
	}
	order, err := services.GetPaidOrderByCustomerByID(orderID)
	if err != nil {
		msg := tgbotapi.NewMessage(senderChatID, err.Error())
		tg_bot.Bot.Send(msg)
		return
	}

	services.SendOrderCallBack(senderChatID, senderMsgID, order, order.Product, order.ProductItem)
}
