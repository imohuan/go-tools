package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Config 配置结构
type Config struct {
	SPTToken     string `json:"spt_token"`
	DBPath       string `json:"db_path"`
	PollInterval int    `json:"poll_interval"`
	RetryCount   int    `json:"retry_count"`
}

func loadConfig() *Config {
	cfg := &Config{
		PollInterval: 5,
		RetryCount:   3,
	}

	cfg.SPTToken = os.Getenv("WXPUSHER_SPT_TOKEN")
	if cfg.SPTToken == "" {
		cfg.SPTToken = os.Getenv("WXPUSHER_TOKEN")
	}
	cfg.DBPath = os.Getenv("WORKBUDDY_DB_PATH")

	if cfg.SPTToken == "" || cfg.DBPath == "" {
		configPath := "config.json"
		if data, err := os.ReadFile(configPath); err == nil {
			var fileCfg Config
			if json.Unmarshal(data, &fileCfg) == nil {
				if cfg.SPTToken == "" {
					cfg.SPTToken = fileCfg.SPTToken
				}
				if cfg.DBPath == "" {
					cfg.DBPath = fileCfg.DBPath
				}
				if fileCfg.PollInterval > 0 {
					cfg.PollInterval = fileCfg.PollInterval
				}
				if fileCfg.RetryCount > 0 {
					cfg.RetryCount = fileCfg.RetryCount
				}
			}
		}
	}

	if cfg.DBPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("无法获取用户主目录: %v", err)
		}
		cfg.DBPath = filepath.Join(home, ".workbuddy", "workbuddy.db")
	}

	if cfg.SPTToken == "" {
		log.Fatal("缺少 WxPusher SPT Token，请设置环境变量 WXPUSHER_SPT_TOKEN 或修改 config.json")
	}
	if cfg.PollInterval < 1 {
		cfg.PollInterval = 5
	}
	if cfg.RetryCount < 1 {
		cfg.RetryCount = 3
	}
	return cfg
}

func main() {
	mode := flag.String("mode", "all", "运行模式: all | monitor | server")
	port := flag.Int("port", 8080, "服务器端口")
	flag.Parse()

	log.SetFlags(log.Ltime)

	// 预先加载 config 获取 dbPath（所有模式共用）
	cfg := loadConfig()
	dbPath := cfg.DBPath

	switch *mode {
	case "all":
		runAll(*port, dbPath)
	case "server":
		StartServer(*port, "", dbPath)
	case "monitor":
		startMonitor(cfg)
	default:
		fmt.Fprintf(os.Stderr, "用法: %s -mode=all|monitor|server [-port=8080]\n", os.Args[0])
		os.Exit(1)
	}
}

// runAll 同时启动 Web 服务和任务监听
func runAll(port int, dbPath string) {
	cfg := loadConfig()

	log.Println("=== CodeBuddy Notify (All-in-One) ===")
	fmt.Printf("数据库:   %s\n", cfg.DBPath)
	fmt.Printf("轮询间隔: %ds\n", cfg.PollInterval)
	fmt.Printf("Web 服务: http://localhost:%d\n", port)
	fmt.Printf("SPT Token: %s***\n", cfg.SPTToken[:min(10, len(cfg.SPTToken))])

	if _, err := os.Stat(cfg.DBPath); os.IsNotExist(err) {
		log.Printf("警告: 数据库不存在 %s，任务监听将在数据库就绪后开始", cfg.DBPath)
	}

	// 启动任务监听（goroutine）
	go func() {
		// 等数据库就绪
		for {
			if _, err := os.Stat(cfg.DBPath); err == nil {
				break
			}
			time.Sleep(time.Second)
		}
		startMonitor(cfg)
	}()

	// 启动 Web 服务（主线程，阻塞）— 内部自动打开浏览器
	StartServer(port, "", dbPath)
}

func startMonitor(cfg *Config) {
	log.SetFlags(log.Ltime)
	log.Println("=== 任务监听启动 ===")

	if _, err := os.Stat(cfg.DBPath); os.IsNotExist(err) {
		log.Fatalf("数据库不存在: %s", cfg.DBPath)
	}

	db, err := OpenSQLite(cfg.DBPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()
	log.Println("数据库连接成功")

	notifier := NewWxPusherNotifier(cfg.SPTToken, cfg.RetryCount)
	monitor := NewSessionMonitor(db)
	monitor.Refresh()

	ticker := time.NewTicker(time.Duration(cfg.PollInterval) * time.Second)
	defer ticker.Stop()

	log.Println("开始监听 session 状态变化...")

	for range ticker.C {
		changes := monitor.Refresh()
		if len(changes) == 0 {
			continue
		}
		for _, change := range changes {
			switch change.Status {
			case "completed":
				notifier.NotifyCompleted(change)
			case "failed", "error":
				notifier.NotifyFailed(change)
			}
		}
	}
}
