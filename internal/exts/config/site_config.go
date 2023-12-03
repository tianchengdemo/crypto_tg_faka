package config

import (
	"encoding/json"
	"gopay/internal/utils/functions"
	"gopkg.in/ini.v1"
	"reflect"
	"strings"
	"sync"
	"time"
)

type SiteConfigStruct struct {
	//EnableReg           bool   `desc:"是否开启注册"`
	TgBotToken            string        `json:"tg_bot_token" desc:"Telegram Bot Token, 在@BotFather申请(重启生效)"`
	AdminTGID             int64         `json:"admin_tg_id" desc:"管理员Telegram Chat ID,可以在@userinfobot获取,管理员可直接登录后台,请勿乱填(重启生效)"`
	Host                  string        `json:"host" desc:"域名，用于生成登录链接和重定向等操作"`
	OrderExpireDuration   time.Duration `json:"order_expire_duration" desc:"订单过期时间,用户支付和链上交易需要时间,不要设置太短"`
	TronGridApiKey        string        `json:"tron_grid_api_key" desc:"TronGrid API密钥,用于监听交易,在此获取:https://www.trongrid.io/dashboard/keys"`
	EnableFixExchangeRate bool          `json:"enable_fix_exchange_rate" desc:"启用固定汇率"`
	FixedExchangeRate     string        `json:"fixed_exchange_rate" desc:"固定汇率(參照首頁實時匯率填寫，測試後再上綫)"`
	//DecimalWalletUnit         string        `validate:"numeric" json:"decimal_wallet_unit" desc:"小数点钱包单位步长,同时也是最小保留小数位数,如0.0001"`
	//DecimalWalletMaxIncrement string        `validate:"numeric" json:"decimal_wallet_max_increment" desc:"小数点钱包最大增量,如0.01,确保在使用小数点尾数钱包的时候,用户多支付的费用不超过该数"`
	//WalletDecimalPlace        int            `validate:"numeric" json:"wallet_decimal_place" desc:"钱包小数点位数,如3则为0.001,4则为0.0001,不要太大,超过货币最大位数会导致用户无法正好付到这个金额"`
	//WalletMaxDecimalIncrement int            `validate:"numeric" json:"WalletMaxDecimalIncrement" desc:"该值乘以钱包小数点最小位数,则为订单最大增量. 如该值为100,小数点最小位数为0.0001,则订单价格不会超过原有价格加上0.01"`

	PaymentMethods   string `json:"payment_methods" desc:"启用的支付方式"`
	WalletType       int    `json:"wallet_type" desc:"收款类型: 1.任意金额钱包 2.小数点尾数钱包"`
	Proxy            Proxy  `json:"proxy" desc:"网络代理，如果要用代理则取消注释并填写"`
	LogLevel         int    `json:"log_level" desc:"日志记录级别,0为Debug"`
	EnableDBDebug    bool   `json:"enable_db_debug" desc:"开启数据库Debug输出(重启生效)"`
	EnableTGBotDebug bool   `json:"enable_tg_bot_debug" desc:"开启Telegram Bot 输出(重启生效)"`
}

func LoadSiteConfig() {
	SiteConfigLock.Lock()
	defer SiteConfigLock.Unlock()

	path := SiteConfigPath
	config := new(SiteConfigStruct)

	cfg, err := ini.Load(path)
	if err != nil {
		panic(err)
	}
	err = cfg.MapTo(&config)
	if err != nil {
		panic(err)
	}

	if config.TronGridApiKey == "" {
		panic("TronGrid API缺失")
	}

	SiteConfig = config
}

func GetSiteConfig() *SiteConfigStruct {
	SiteConfigLock.RLock()
	defer SiteConfigLock.RUnlock()
	return SiteConfig
}

func SetSiteConfig(changedSiteConfig SiteConfigStruct) error {
	path := SiteConfigPath

	changedSiteConfigValue := changedSiteConfig
	changedSiteConfigPointer := &changedSiteConfigValue

	//// struct映射cfg前,要排除nil的属性,否则panic
	//valForMap := reflect.ValueOf(changedSiteConfigPointer).Elem()
	//for i := 0; i < valForMap.NumField(); i++ {
	//	field := valForMap.Field(i)
	//	if field.Kind() == reflect.Ptr && field.IsNil() {
	//		fieldType := field.Type()
	//		newValue := reflect.New(fieldType.Elem())
	//		field.Set(newValue)
	//	}
	//}

	cfg := ini.Empty()
	err := ini.ReflectFrom(cfg, changedSiteConfigPointer)
	if err != nil {
		return err
	}

	// 写描述
	val := reflect.ValueOf(changedSiteConfigPointer).Elem()
	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		//if !field.CanInterface() {
		//	continue
		//}
		fieldName := typ.Field(i).Name
		desc := typ.Field(i).Tag.Get("desc")

		// time.Duration用string保存,如30m
		if field.Type() == reflect.TypeOf(time.Duration(0)) {
			cfg.Section("").Key(fieldName).SetValue(field.Interface().(time.Duration).String())
		}
		//// decimal用string保存,不然会被识别成struct,建成单独一个section
		//if field.Type() == reflect.TypeOf(decimal.Decimal{}) {
		//	cfg.Section("").Key(fieldName).SetValue(field.Interface().(decimal.Decimal).String())
		//}

		// 全局说明
		cfg.Section("").Comment = ""

		if desc != "" {
			// 如果是struct，则描述写在section上
			if (field.Kind() == reflect.Struct) || (field.Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.Struct) {
				cfg.Section(fieldName).Comment = desc

				// struct嵌套写描述
				section := cfg.Section(fieldName)
				val := field
				typ := val.Type()
				for i := 0; i < val.NumField(); i++ {
					//if !val.Field(i).CanInterface() {
					//	continue
					//}
					fieldName := typ.Field(i).Name
					desc := typ.Field(i).Tag.Get("desc")
					if desc != "" {
						section.Key(fieldName).Comment = desc
					}

				}

			} else {
				cfg.Section("").Key(fieldName).Comment = desc
			}
		}

	}
	err = cfg.SaveTo(path)
	if err != nil {
		return err
	}

	LoadSiteConfig()

	return nil
}

type RespConfig struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
	Name  string      `json:"name"`
	Desc  string      `json:"desc"`
	Type  string      `json:"type"`
}

func ConfigToMap(config *SiteConfigStruct) []map[string]interface{} {
	val := reflect.Indirect(reflect.ValueOf(config)) // Automatically handles pointers
	var respConfigmaps []map[string]interface{}
	//val := valPtr.Elem()

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldName := val.Type().Field(i).Name

		var respConfigMap map[string]interface{}
		respConfig := new(RespConfig)

		// 改写某些类型的值,方便查看
		var fieldValue interface{}
		if field.Type() == reflect.TypeOf(time.Duration(0)) {
			fieldValue = field.Interface().(time.Duration).String()
		} else {
			fieldValue = field.Interface()
		}
		//else if field.Type() == reflect.TypeOf(decimal.Decimal{}) {
		//	fieldName = field.Interface().(decimal.Decimal).String()
		//}

		// 改写类型给前端显示
		var fieldType string
		//if field.Type() == reflect.TypeOf(decimal.Decimal{}) {
		//	fieldType = "string"
		//} else
		if field.Type() == reflect.TypeOf(time.Duration(0)) {
			fieldType = "string"
		} else if field.Kind() >= reflect.Int && field.Kind() <= reflect.Int64 {
			// 这个药房duration后面,不然time.duration会被识别成int
			fieldType = "int"
		} else if field.Kind() == reflect.Struct {
			fieldType = "json"
		} else {
			fieldType = field.Type().String()
		}

		// 固定汇率前端用json,后端接收后转为string储存
		if fieldName == "FixedExchangeRate" {
			err := json.Unmarshal([]byte(fieldValue.(string)), &fieldValue)
			if err != nil {
				fieldValue = make(map[string]interface{})
			}
			fieldType = "json"
		}

		jsonTag := val.Type().Field(i).Tag.Get("json")
		tagParts := strings.Split(jsonTag, ",")
		jsonKey := tagParts[0]

		respConfig.Key = jsonKey
		respConfig.Name = fieldName
		respConfig.Value = fieldValue
		respConfig.Desc = val.Type().Field(i).Tag.Get("desc")
		respConfig.Type = fieldType

		respConfigMap = functions.StructToMap(respConfig, functions.StructToMapExcludeMode)
		respConfigmaps = append(respConfigmaps, respConfigMap)
	}

	return respConfigmaps
}

var SiteConfig *SiteConfigStruct
var SiteConfigLock = &sync.RWMutex{}

var SendAdminLimit = time.NewTicker(5 * time.Second)
