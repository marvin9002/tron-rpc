package trx

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tron/log"

	"github.com/BurntSushi/toml"
	"github.com/shopspring/decimal"
)

var ctx, canceltask = context.WithCancel(context.Background())
var wg sync.WaitGroup

var port = "9290"
var trxdecimal int32 = 6

var minScanBlock int64 = 23513066 // 最小 扫描高度
var targetHeight int64
var blockHeightTop int64
var minAmount decimal.Decimal
var remainAmount = decimal.New(10, 0) // 保留10个

var goroutineNumScan int64 = 4 // 扫描交易记录的并发携程数

var keystore = "."               // 钱包文件
var mainAddr = ""                // 主地址
var mainAccout *ecdsa.PrivateKey // 主地址密钥

// 归集参数
var minFee = decimal.New(3, 0) // 每个地址至少保留多少trx手续费
var perFee = decimal.New(3, 0) // 每次归集每个合约需要手续费消耗

// 为了替换前一个 TODO:
var mainAddr1 = ""                // 主地址2
var mainAccout1 *ecdsa.PrivateKey // 主地址密钥2
var istwomain bool                // 是否两个主地址 第一个主地址还是负责提币和转手续费 第二个的地址负责收集归集币

var dbengine *DB // 数据库连接

var globalConf GlobalConf

var curr = getCurrentDirectory() + `/`

func getCurrentDirectory() string {
	dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		return "."
	}
	return strings.Replace(dir, "\\", "/", -1)
}

// Init 初始化
func Init() {
	if _, err := toml.DecodeFile(curr+"trx.toml", &globalConf); err != nil {
		fmt.Println(err)
		_, err = toml.Decode(string(getConfig()), &globalConf)
		if err != nil {
			panic(err)
		}
	}
	InitLog() // 首先初始化日志
	keystore, _ = filepath.Abs(globalConf.Client.KeyStore)
	var err error

	err = InitMainNode(globalConf.Client.NodeTrx)
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}

	InitAllNode(globalConf.Client.NodeUrl)

	err = InitContract(globalConf.Contracts)
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}

	if globalConf.Client.Port != "" {
		port = globalConf.Client.Port
	}

	minAmount = globalConf.Collection.MinAmount

	minScanBlock = globalConf.Scantraderecord.MinScanBlock

	if globalConf.Scantraderecord.GoroutineNum > 0 {
		goroutineNumScan = globalConf.Scantraderecord.GoroutineNum
	}

	if globalConf.Client.Count > 0 && globalConf.Client.Count < 100 {
		count = globalConf.Client.Count
	}
	// 最大100trx
	if globalConf.Client.Feelimit > 0 && globalConf.Client.Feelimit < 100000000 {
		feelimit = globalConf.Client.Feelimit
	}

	if globalConf.Client.MinFee.Cmp(decimal.Zero) > 0 {
		minFee = globalConf.Client.MinFee
	}

	if globalConf.Client.PerFee.Cmp(decimal.Zero) > 0 {
		perFee = globalConf.Client.PerFee
	}

	mainAddr = globalConf.Client.MainAddr
	mainAccout, err = loadAccountWithUUID(mainAddr, globalConf.Client.Password)
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}
	// mainAddr1 = globalConf.Client.MainAddr1
	// if mainAddr1 != "" {
	// 	mainAccout1, err = loadAccountWithUUID(mainAddr1, globalConf.Client.Password1)
	// 	if err != nil {
	// 		log.Error(err)
	// 		os.Exit(1)
	// 	}
	// 	istwomain = true
	// }

	dbengine, err = InitDB(globalConf.Client.DBAddr)
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}
	targetHeight = getlastBlock()

	log.Info("lastblock:", targetHeight)

	err = getWalletInfo()
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}

	log.Info("walletInfo:", walletInfo)

	task()
}

func task() {
	var scanT = globalConf.Scantraderecord.TimeIntervalSec
	if scanT < 1 {
		scanT = 60
	}
	var collectT = globalConf.Collection.TimeIntervalMin
	if collectT < 1 {
		collectT = 30
	}
	var timed = time.Duration(scanT) * time.Second
	var timec = time.Duration(collectT) * time.Minute
	tiker := time.NewTicker(timed)
	tikerT := time.NewTicker(2 * timed)
	tikerC := time.NewTicker(timec)
	wg.Add(3)
	go func() {
		log.Info("RunTransaction Ticker")
		for {
			select {
			case <-ctx.Done():
				wg.Done()
				tiker.Stop()
				return
			case <-tiker.C:
				log.Debug("start RunTransaction")
				RunTransaction()
				log.Debug("stop RunTransaction")
			}
		}
	}()
	go func() {
		log.Info("getWalletInfo Ticker")
		for {
			select {
			case <-ctx.Done():
				wg.Done()
				tikerT.Stop()
				return
			case <-tikerT.C:
				log.Debug("start getWalletInfo")
				getWalletInfo()
				log.Debug("stop getWalletInfo")
			}
		}
	}()
	go func() {
		log.Info("RunCollect Ticker")
		for {
			select {
			case <-ctx.Done():
				wg.Done()
				tikerC.Stop()
				return
			case <-tikerC.C:
				log.Debug("start RunCollect")
				RunCollect()
				log.Debug("stop RunCollect")
			}
		}
	}()
}

//获取默认的数据库配置
func getConfig() []byte {
	return []byte(`

# grpc.trongrid.io:50051
# 3.225.171.164:50051
# grpc.shasta.trongrid.io:50051
[client]
nodeTrx="grpc.trongrid.io:50051"
main_addr="TQCknYutmcMxGoq32JqQWvn1MzyRfuQirC" #主钱包地址
password="eb1804aa-fa7d-4782-8145-afe4da83c56d" #主钱包秘钥加密前的密码 uuid
#THqoopDxSfDSUu4G7EqAYX1CdmjXDZMWNG
#bf8926a4-767e-4734-a74c-0511cf997b17
key_store="D:/go/src/tron/trx/key_store" #用户秘钥保存路径 从运行文件路径开始算 默认 key_store
db_addr="D:/go/src/tron/trx/trx.db"
port="9291"
logLevel="info" # 日志等级默认
feelimit=2000000

# 合约配置 
[[contract]]
name="USDT"  # 暂时没有用到
type="trc20" # 合约类型
contract="TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t" # trc20 合约地址
issuer="THPvaUhoh2Qn2y9THCZML3H815hhFhn5YC" # 发行者地址 暂时没有用到
port="9292" # 监听端口
min_amount=5 # 最小归集数量
decimal=6 # 币种小数位

# [[contract]]
# name="BTT"
# type="trc10" # 合约类型
# contract="1002000" # 合约配置 trc10 合约ID assertname
# issuer="TF5Bn4cJCT6GVeUgyCN4rBhDg42KBrpAjg" # 发行者地址 暂时没有用到
# port="9293" # 监听端口
# min_amount=0.1 # 最小归集数量

[collection]
time_interval_min=60 # 归集检测间隔  单位 分
min_amount=10 # 最小归集钱包余额 单位TRX 后面 6个零 1TRX =10^6 SUN

[scantraderecord]
time_interval_sec=5 # 扫描交易记录检测间隔 单位秒
# 扫描交易记录起始位置 如果配置为正数 
# 如果为负数 则取绝对值 从绝对值位置开始扫描，不取最大值开始扫描
min_scan_block = 23520251
goroutine_num=4 # 每次扫描开的协程数量
`)
}
