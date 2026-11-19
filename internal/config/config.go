package config

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config 保存运行期配置，全部来自环境变量，未设置时使用本地开发默认值。
type Config struct {
	Addr             string
	DBDriver         string
	DSN              string
	StorageDriver    string
	StorageDir       string
	BaseURL          string
	Secret           string
	MaxUploadMB      int
	MaxFilesPerBatch int
	MaxRoomPhotos    int
	RetentionDays    int
}

func Load() (Config, error) {
	c := Config{
		Addr:             env("PHOTOLIVE_ADDR", "0.0.0.0:8080"),
		DBDriver:         strings.ToLower(env("PHOTOLIVE_DB_DRIVER", "sqlite")),
		DSN:              os.Getenv("PHOTOLIVE_DB_DSN"),
		StorageDriver:    strings.ToLower(env("PHOTOLIVE_STORAGE", "local")),
		StorageDir:       env("PHOTOLIVE_STORAGE_DIR", "./data/media"),
		BaseURL:          strings.TrimRight(os.Getenv("PHOTOLIVE_BASE_URL"), "/"),
		MaxUploadMB:      envInt("PHOTOLIVE_MAX_UPLOAD_MB", 50),
		MaxFilesPerBatch: envInt("PHOTOLIVE_MAX_FILES", 300),
		MaxRoomPhotos:    envInt("PHOTOLIVE_MAX_ROOM_PHOTOS", 3000),
		RetentionDays:    envInt("PHOTOLIVE_RETENTION_DAYS", 30),
	}

	// 未显式配置对外地址时，自动使用本机局域网 IP，保证手机扫码即可访问
	if c.BaseURL == "" {
		if ip := localIP(); ip != "" {
			c.BaseURL = "http://" + net.JoinHostPort(ip, portOf(c.Addr))
		}
	}

	if c.DBDriver == "sqlite" && c.DSN == "" {
		if err := os.MkdirAll("./data", 0o755); err != nil {
			return c, err
		}
		c.DSN = "file:./data/photolive.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	}

	secret, err := loadSecret()
	if err != nil {
		return c, err
	}
	c.Secret = secret
	return c, nil
}

// loadSecret 优先读取环境变量；否则在 data/secret.key 中持久化一份随机密钥，
// 保证重启后登录态与房间访问凭证仍然有效。
func loadSecret() (string, error) {
	if s := os.Getenv("PHOTOLIVE_SECRET"); s != "" {
		return s, nil
	}
	if err := os.MkdirAll("./data", 0o755); err != nil {
		return "", err
	}
	p := filepath.Join("data", "secret.key")
	if b, err := os.ReadFile(p); err == nil && len(b) >= 32 {
		return strings.TrimSpace(string(b)), nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	s := hex.EncodeToString(buf)
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		return "", err
	}
	return s, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// localIP 返回本机在局域网中的 IPv4 地址，优先私有网段。
func localIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	fallback := ""
	skipNames := []string{"vmware", "virtualbox", "vethernet", "hyper-v", "wsl", "docker", "tailscale", "zerotier", "loopback"}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(iface.Name)
		virtual := false
		for _, s := range skipNames {
			if strings.Contains(name, s) {
				virtual = true
				break
			}
		}
		if virtual {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			if ip.IsPrivate() {
				return ip.String()
			}
			if fallback == "" {
				fallback = ip.String()
			}
		}
	}
	return fallback
}

func portOf(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err == nil && port != "" {
		return port
	}
	return "8080"
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}
