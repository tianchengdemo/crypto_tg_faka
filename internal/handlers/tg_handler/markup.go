package tg_handler

import (
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"gopay/internal/exts/config"
	"gopay/internal/models"
	"gopay/internal/services"
)

func paginationToRows(pagination services.Pagination) [][]tgbotapi.InlineKeyboardButton {
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, item := range pagination.Items {
		product := item.(models.Product)
		buttonText := fmt.Sprintf("%s : %s 库存:%d", product.Name, product.Description, product.InStockCount)

		row := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(buttonText, ProductDetailPrefix+product.ID.String())}
		rows = append(rows, row)
	}

	var paginationRow []tgbotapi.InlineKeyboardButton
	if pagination.Page > 1 {
		paginationRow = append(paginationRow, tgbotapi.NewInlineKeyboardButtonData("上一页", ProductListPagePrefix+fmt.Sprintf("%d", pagination.Page-1)))
	}
	if int64(pagination.Page) < pagination.TotalPage {
		paginationRow = append(paginationRow, tgbotapi.NewInlineKeyboardButtonData("下一页", ProductListPagePrefix+fmt.Sprintf("%d", pagination.Page+1)))
	}
	// row不能为空，空了发不出去
	if len(paginationRow) != 0 {
		rows = append(rows, paginationRow)
	}

	return rows
}

func paymentSelectRow(productID uuid.UUID) []tgbotapi.InlineKeyboardButton {
	var paymentSelectRow []tgbotapi.InlineKeyboardButton
	for _, v := range config.GetAvailablePaymentMethods() {
		callbackData := fmt.Sprintf("%s%s_%s", PayOrderPrefix, productID, v)
		paymentSelectRow = append(paymentSelectRow, tgbotapi.NewInlineKeyboardButtonData(v, callbackData))
	}
	return paymentSelectRow
}
func deleteMsgRow() []tgbotapi.InlineKeyboardButton {
	var paymentSelectRow []tgbotapi.InlineKeyboardButton
	paymentSelectRow = append(paymentSelectRow, tgbotapi.NewInlineKeyboardButtonData("关闭", "delete_msg"))

	return paymentSelectRow
}

func GoBackRow(callBackData string) []tgbotapi.InlineKeyboardButton {
	goBackRow := []tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("返回", callBackData)}
	return goBackRow
}
