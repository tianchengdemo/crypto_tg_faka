package services

import (
	"errors"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gopay/internal/exts/config"
	"gopay/internal/exts/db"
	"gopay/internal/exts/tg_bot"
	"gopay/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func CreateOrder(targetCurrency config.Currency, targetNetwork string, product models.Product, tgChatID int64, tgUsername string) (*models.Order, error) {
	// 基础金额,需换算
	baseCurrency := product.Currency
	baseCurrencyPrice := product.Price

	targetPrice, err := config.ConvertCurrencyPrice(baseCurrencyPrice, config.Currency(baseCurrency), targetCurrency)
	if err != nil {
		return nil, errors.New("获取汇率失败")
	}

	// 精度截断，当精度大于小数尾数步长，则截断，否则保留精度
	targetPrice = targetPrice.Round(-config.DecimalWalletUnitMap[targetCurrency].Exponent())

	// 使用tx.begin应该慎重，需要显式commit或rollback，不然会导致会话过多塞满数据库
	tx := db.DB.Begin()
	defer tx.Rollback()

	// 获取空闲钱包,分为1.任意金额钱包 2.小数点尾数钱包,
	// 任意金额钱包要锁,绑定订单后状态会从1变成0
	// 并获取最后的订单价格
	var orderFinalPrice *decimal.Decimal
	var priceIDForLock *string

	var freeWallet *models.Wallet
	walletType := config.SiteConfig.WalletType
	if walletType == 1 {
		orderFinalPrice = &targetPrice
		// 获取钱包并上锁，因为这个钱包状态需要更改的
		if result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("status=? and network=?", 1, targetNetwork).Order(GetWalletOrder()).Find(&freeWallet); result.Error != nil {
			return nil, errors.New("获取钱包出错")
		} else if result.RowsAffected == 0 {
			return nil, errors.New("无空闲钱包1")
		}
		// 修改钱包状态为锁定
		if result := tx.Model(&models.Wallet{}).Where("id=?", freeWallet.ID).Update("status", 0); result.Error != nil {
			return nil, err
		} else if result.RowsAffected == 0 {
			return nil, errors.New("钱包状态出错")
		}

	} else if walletType == 2 {
		freeWallet, err = GetFreeDecimalWallet(targetNetwork, targetCurrency, targetPrice)
		if err != nil {
			return nil, err
		}
		// 获取最终的订单价格
		orderFinalPrice, err = GetFreeDecimalWalletPrice(targetNetwork, targetCurrency, freeWallet.ID, targetPrice)
		if err != nil {
			return nil, errors.New("获取最终订单价格失败: " + err.Error())
		}
		// 无法从函数获得指针，需要变量中转
		temp := fmt.Sprintf("%s-%s-%s-%s", freeWallet.Address, targetNetwork, targetCurrency, orderFinalPrice)
		priceIDForLock = &temp

	} else {
		return nil, errors.New("钱包类型设置错误")
	}

	// 商品库存,获取一个空闲商品项目,并锁定
	var productItem models.ProductItem
	if result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("product_id=? and status=1", product.ID).Find(&productItem); result.Error != nil {
		return nil, err
	} else if result.RowsAffected == 0 {
		return nil, errors.New("商品无库存")
	}

	end_time := time.Now().Unix() + int64(config.SiteConfig.OrderExpireDuration.Seconds())

	// 创建订单
	order := models.NewOrder(end_time, string(targetCurrency), targetNetwork, *orderFinalPrice, priceIDForLock, baseCurrency, baseCurrencyPrice, freeWallet.ID, freeWallet.Address, walletType, product.ID, tgChatID, tgUsername)
	tx.Create(order)

	// 更新项目为待支付,设置解锁时间,并绑定到订单上,要在订单创建的事务之后
	if result := tx.Model(&models.ProductItem{}).Where("id=?", productItem.ID).Updates(map[string]interface{}{
		"status":        0,
		"order_id":      order.ID,
		"end_lock_time": end_time,
	}); result.Error != nil {
		return nil, err
	} else if result.RowsAffected == 0 {
		return nil, errors.New("商品项目更新失败")
	}

	// 设置钱包解锁时间
	if result := tx.Model(&models.Wallet{}).Where("id=?", freeWallet.ID).Updates(map[string]interface{}{
		"end_lock_time": end_time,
	}); result.Error != nil {
		return nil, err
	}

	if err := tx.Commit().Error; err != nil {
		return nil, errors.New("提交失败, " + err.Error())
	}

	UpdateProductInStockCount([]uuid.UUID{product.ID})
	return order, nil
}

func ClearExpireOrder() error {

	// 设置订单过期
	if result := db.DB.Model(&models.Order{}).Where("status = 0 and end_time < ?", time.Now().Unix()).Updates(map[string]interface{}{
		"status":            -1,
		"end_time":          time.Now().Unix(),
		"price_id_for_lock": gorm.Expr("NULL"),
	}); result.Error != nil {
		return errors.New("设置订单过期失败")
	}

	return nil
}

// 强行关闭订单
func ReleaseOrders(toReleaseOrderIDsInput []uuid.UUID) error {
	// 如果不判斷訂單已過期會導致後面的解鎖項目出問題，商品項目售出會解鎖重新出售，錢包會無故解鎖
	var toReleaseOrders []models.Order
	if result := db.DB.Where("status = 0 and id in ?", toReleaseOrderIDsInput).Find(&toReleaseOrders); result.Error != nil {
		return errors.New("查询订单失败")
	}
	var toReleaseOrderIDs []uuid.UUID
	for _, toReleaseOrders := range toReleaseOrders {
		toReleaseOrderIDs = append(toReleaseOrderIDs, toReleaseOrders.ID)
	}
	if len(toReleaseOrderIDs) == 0 {
		return nil
	}

	tx := db.DB.Begin()
	defer tx.Rollback()
	// 设置订单失效
	if result := db.DB.Model(&models.Order{}).Where("id in ?", toReleaseOrderIDs).Updates(map[string]interface{}{
		"status":            -2,
		"end_time":          time.Now().Unix(),
		"price_id_for_lock": gorm.Expr("NULL"),
	}); result.Error != nil {
		return errors.New("设置订单关闭失败")
	}

	// 解锁钱包,只需更新status为0的钱包
	var toReleaseWalletIDs []uuid.UUID
	for _, toReleaseOrder := range toReleaseOrders {
		toReleaseWalletIDs = append(toReleaseWalletIDs, toReleaseOrder.WalletID)
	}

	if result := tx.Model(&models.Wallet{}).Where("status = 0 and id in ?", toReleaseWalletIDs).Updates(map[string]interface{}{
		"status": 1,
	}); result.Error != nil {
		return errors.New("解锁钱包失败")
	}

	// 解锁商品项目,取消绑定订单
	if result := tx.Model(&models.ProductItem{}).Where("order_id in ?", toReleaseOrderIDs).Updates(map[string]interface{}{
		"status":        1,
		"order_id":      gorm.Expr("NULL"),
		"end_lock_time": gorm.Expr("NULL"),
	}); result.Error != nil {
		return errors.New("解锁商品项目失败")
	}
	if err := tx.Commit().Error; err != nil {
		return errors.New("清理过期订单提交失败, " + err.Error())
	}

	// 更新商品库存
	var productIDs []uuid.UUID
	for _, expiredOrder := range toReleaseOrders {
		productIDs = append(productIDs, expiredOrder.ProductID)
	}
	UpdateProductInStockCount(productIDs)

	// 删消息
	for _, expiredOrder := range toReleaseOrders {
		deleteConfig := tgbotapi.DeleteMessageConfig{
			ChatID:    expiredOrder.TGChatID,
			MessageID: int(expiredOrder.TGMsgID),
		}
		tg_bot.Bot.Request(deleteConfig)
	}

	return nil
}

func OrderPaidPrice(order models.Order, options ...interface{}) decimal.Decimal {
	// 查已支付，没有更新，只是借用tx事务进行查询，因此不用commit
	tx := db.DB
	for _, value := range options {
		if opt, ok := value.(*gorm.DB); ok {
			tx = opt
		}
	}

	var result decimal.Decimal
	var transfers []models.Transfer
	tx.Where("order_id = ?", order.ID).Find(&transfers)

	for _, transfer := range transfers {
		result = result.Add(transfer.Price)
	}

	return result
}

func OrderCallbackMultiple(successOrderIDs []uuid.UUID) error {
	var successOrders []models.Order
	db.DB.Preload("Product").Preload("ProductItem").Where("id in ?", successOrderIDs).Find(&successOrders)
	for _, successOrder := range successOrders {
		// 发消息
		SendOrderCallBack(successOrder.TGChatID, int(successOrder.TGMsgID), successOrder, successOrder.Product, successOrder.ProductItem)
	}

	return nil
}
func SetOrderTGMsgID(orderID uuid.UUID, tgMsgID int64) {
	db.DB.Model(&models.Order{}).Where("id = ?", orderID).Update("tg_msg_id", tgMsgID)
}

func GetOrderIncomeByTimestampRange(startTimestamp int64, endTimestamp int64) (decimal.Decimal, error) {
	var orders []models.Order
	filterParams := make(map[string]interface{})
	filterParams["timestamp_range"] = fmt.Sprintf("%d,%d", startTimestamp, endTimestamp)
	query := models.ApplyFilters(db.DB, filterParams).Where("status=1")
	if result := query.Find(&orders); result.Error != nil {
		return decimal.Decimal{}, errors.New("获取订单错误")
	}

	orderPriceSum := decimal.Zero
	for _, order := range orders {
		convertedPrice, err := config.ConvertCurrencyPrice(order.Price, config.Currency(order.Currency), config.CNY)
		if err != nil {
			return decimal.Decimal{}, errors.New("获取汇率失败")
		}
		orderPriceSum = orderPriceSum.Add(convertedPrice)
	}

	return orderPriceSum, nil
}
func GetPaidOrdersByCustomer(tgChatID int64) ([]models.Order, error) {
	var orders []models.Order
	if result := db.DB.Preload("Product").Preload("ProductItem").Where("status = 1 and tg_chat_id = ?", tgChatID).Order("create_time desc").Limit(10).Find(&orders); result.Error != nil {
		return orders, errors.New("获取订单错误")
	}

	return orders, nil
}
func GetPaidOrderByCustomerByID(orderID uuid.UUID) (models.Order, error) {
	var orders models.Order
	if result := db.DB.Preload("Product").Preload("ProductItem").Where("id = ?", orderID).Find(&orders); result.Error != nil {
		return orders, errors.New("获取订单错误")
	} else if result.RowsAffected == 0 {
		return orders, errors.New("没有该订单")
	}
	return orders, nil
}
func SendOrderCallBack(chatID int64, toDeleteMsgID int, order models.Order, product models.Product, productItem models.ProductItem) {
	msgText := config.OrderCallbackMsg(map[string]interface{}{
		"Order":       order,
		"Product":     product,
		"ProductItem": productItem,
	})
	//newMsg := tgbotapi.NewEditMessageText(chatID, msgID, msgText)
	msg := tgbotapi.NewMessage(chatID, msgText)
	tg_bot.Bot.Send(msg)

	if toDeleteMsgID != 0 {
		tg_bot.DeleteMsg(chatID, toDeleteMsgID)
	}
}
