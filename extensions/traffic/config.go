// Package traffic is an optional, independent read-only traffic collector.
// It never opens MikroDash's data directory or modifies a router.
package traffic

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type ROSConfig struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password,omitempty"`
	TLS         bool   `json:"tls"`
	InsecureTLS bool   `json:"insecureTLS"`
}
type Config struct {
	RouterID       string    `json:"routerId"`
	Enabled        bool      `json:"enabled"`
	ClashURL       string    `json:"clashURL"`
	ClashSecret    string    `json:"clashSecret,omitempty"`
	ROS            ROSConfig `json:"ros"`
	Infrastructure []string  `json:"infrastructure"`
}

func privateHost(host string) bool {
	ip, err := netip.ParseAddr(host)
	return err == nil && (ip.IsPrivate() || ip.IsLoopback())
}
func (c *Config) Validate() error {
	if c.RouterID == "" || len(c.RouterID) > 128 {
		return errors.New("请选择路由器")
	}
	if c.ClashURL != "" {
		u, e := url.Parse(c.ClashURL)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || !privateHost(u.Hostname()) {
			return errors.New("OpenClash 地址须为内网 IP 的 http/https 根地址，不含密码、路径或查询参数")
		}
		c.ClashURL = strings.TrimRight(c.ClashURL, "/")
	}
	if c.ROS.Host != "" && (!privateHost(c.ROS.Host) || c.ROS.Port < 1 || c.ROS.Port > 65535 || c.ROS.Username == "") {
		return errors.New("请检查 ROS 内网 IP、API 端口和用户名")
	}
	if len(c.ClashSecret) > 4096 || len(c.ROS.Password) > 4096 || len(c.ROS.Username) > 128 || len(c.Infrastructure) > 128 {
		return errors.New("配置字段过长")
	}
	for i, v := range c.Infrastructure {
		v = strings.TrimSpace(v)
		if ip, e := netip.ParseAddr(v); e == nil {
			c.Infrastructure[i] = ip.String()
			continue
		}
		if mac, e := net.ParseMAC(v); e == nil {
			c.Infrastructure[i] = strings.ToUpper(mac.String())
			continue
		}
		return errors.New("基础设施列表只接受 IP 或 MAC 地址，每行一个")
	}
	if c.Enabled && c.ClashURL == "" && c.ROS.Host == "" {
		return errors.New("至少配置一个采集来源")
	}
	return nil
}
func (c Config) Public() map[string]any {
	hasClash, hasROS := c.ClashSecret != "", c.ROS.Password != ""
	c.ClashSecret = ""
	c.ROS.Password = ""
	return map[string]any{"config": c, "hasClashSecret": hasClash, "hasROSPassword": hasROS}
}

// The key and encrypted configurations belong ONLY to the extension volume.
func openKey(dir string) (cipher.AEAD, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	path := filepath.Join(dir, ".key")
	key, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		// Do not replace a lost encryption key over an existing database.
		if _, statErr := os.Stat(filepath.Join(dir, "traffic.db")); statErr == nil {
			return nil, errors.New("extension key missing; restore it together with the database")
		}
		key = make([]byte, 32)
		if _, e = rand.Read(key); e != nil {
			return nil, e
		}
		var f *os.File
		f, e = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return nil, e
		}
		_, e = f.Write(key)
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
	}
	if e != nil {
		return nil, e
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(b)
}
func seal(a cipher.AEAD, c Config) ([]byte, error) {
	raw, e := json.Marshal(c)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, a.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return a.Seal(nonce, nonce, raw, []byte(c.RouterID)), nil
}
func unseal(a cipher.AEAD, id string, b []byte) (Config, error) {
	var c Config
	if len(b) < a.NonceSize() {
		return c, errors.New("invalid sealed config")
	}
	raw, e := a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte(id))
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(raw, &c)
	return c, e
}
