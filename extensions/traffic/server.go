package traffic

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"time"
)

//go:embed ui/*
var assets embed.FS

type Session struct {
	Caps struct {
		ManageSettings bool `json:"manageSettings"`
		Routers        struct {
			Readable   []string `json:"readable"`
			History    []string `json:"history"`
			Manageable []string `json:"manageable"`
		} `json:"routers"`
	} `json:"caps"`
}
type Server struct {
	manager    *Manager
	upstream   *url.URL
	authClient *http.Client
	proxy      *httputil.ReverseProxy
}

func NewServer(m *Manager, upstream string) (*Server, error) {
	u, e := url.Parse(upstream)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Path != "" || u.RawQuery != "" {
		return nil, errors.New("invalid MikroDash upstream")
	}
	s := &Server{manager: m, upstream: u, authClient: newHTTPClient()}
	s.authClient.Timeout = 4 * time.Second
	s.proxy = &httputil.ReverseProxy{Transport: newHTTPClient().Transport, Rewrite: func(r *httputil.ProxyRequest) { r.SetURL(u); r.Out.Host = r.In.Host; r.SetXForwarded() }, ErrorHandler: func(w http.ResponseWriter, r *http.Request, e error) { apiError(w, 502, "MikroDash 暂不可用") }}
	return s, nil
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func apiError(w http.ResponseWriter, status int, message string) {
	jsonOut(w, status, map[string]string{"error": message})
}
func (s *Server) session(r *http.Request) (*Session, error) {
	req, e := http.NewRequestWithContext(r.Context(), http.MethodGet, s.upstream.String()+"/api/auth/status", nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Cookie", r.Header.Get("Cookie"))
	resp, e := s.authClient.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("auth upstream failed")
	}
	var body struct {
		Session *Session `json:"session"`
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&body); e != nil {
		return nil, e
	}
	return body.Session, nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ext := strings.HasPrefix(r.URL.Path, "/api/traffic-extension/") || strings.HasPrefix(r.URL.Path, "/extensions/traffic/")
	if !ext {
		s.proxy.ServeHTTP(w, r)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Cache-Control", "no-store")
	sess, e := s.session(r)
	if e != nil {
		apiError(w, 503, "登录状态验证暂不可用")
		return
	}
	if sess == nil {
		if r.URL.Path == "/extensions/traffic/" {
			http.Redirect(w, r, "/login", http.StatusFound)
		} else {
			apiError(w, 401, "请先登录 MikroDash")
		}
		return
	}
	if strings.HasPrefix(r.URL.Path, "/extensions/traffic/") {
		if r.Method != "GET" && r.Method != "HEAD" {
			apiError(w, 405, "仅接受读取请求")
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'self'; base-uri 'none'; form-action 'none'")
		sub, _ := fs.Sub(assets, "ui")
		http.StripPrefix("/extensions/traffic/", http.FileServer(http.FS(sub))).ServeHTTP(w, r)
		return
	}
	action := strings.TrimPrefix(r.URL.Path, "/api/traffic-extension/")
	if action == "meta" && r.Method == "GET" {
		jsonOut(w, 200, map[string]any{"readable": sess.Caps.Routers.Readable, "history": sess.Caps.Routers.History, "manageable": sess.Caps.Routers.Manageable, "manageSettings": sess.Caps.ManageSettings})
		return
	}
	router := r.URL.Query().Get("router")
	if !slices.Contains(sess.Caps.Routers.Readable, router) {
		apiError(w, 403, "没有此路由器的查看权限")
		return
	}
	switch {
	case action == "live" && r.Method == "GET":
		c := s.manager.Config(router)
		jsonOut(w, 200, map[string]any{"enabled": c.Enabled, "hasClash": c.ClashURL != "", "hasROS": c.ROS.Host != "", "live": s.manager.Snapshot(router)})
	case action == "report" && r.Method == "GET":
		if !slices.Contains(sess.Caps.Routers.History, router) {
			apiError(w, 403, "没有此路由器的历史统计权限")
			return
		}
		from, to, e := period(r.URL.Query(), time.Now())
		if e != nil {
			apiError(w, 400, e.Error())
			return
		}
		out, e := s.manager.db.Report(router, from, to)
		if e != nil {
			apiError(w, 500, "历史数据库读取失败")
			return
		}
		infra := s.manager.Config(router).Infrastructure
		for i := range out.Devices {
			out.Devices[i].Infrastructure = slices.Contains(infra, out.Devices[i].MAC) || slices.Contains(infra, out.Devices[i].IP)
		}
		jsonOut(w, 200, out)
	case action == "config":
		if !sess.Caps.ManageSettings || !slices.Contains(sess.Caps.Routers.Manageable, router) {
			apiError(w, 403, "需要设置管理权限及此路由器的管理权限")
			return
		}
		old := s.manager.Config(router)
		if r.Method == "GET" {
			jsonOut(w, 200, old.Public())
			return
		}
		if r.Method != "POST" {
			apiError(w, 405, "不支持此方法")
			return
		}
		origin, err := url.Parse(r.Header.Get("Origin"))
		if err != nil || origin.Host != r.Host || (origin.Scheme != "http" && origin.Scheme != "https") || r.Header.Get("X-Traffic-Extension") != "1" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			apiError(w, 403, "请求来源验证失败")
			return
		}
		var in struct {
			Config           Config `json:"config"`
			ClearClashSecret bool   `json:"clearClashSecret"`
			ClearROSPassword bool   `json:"clearROSPassword"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
		dec.DisallowUnknownFields()
		if e := dec.Decode(&in); e != nil {
			apiError(w, 400, "配置格式错误")
			return
		}
		c := in.Config
		c.RouterID = router
		if c.ClashSecret == "" && !in.ClearClashSecret {
			if old.ClashURL != "" && strings.TrimRight(c.ClashURL, "/") != old.ClashURL && old.ClashSecret != "" {
				apiError(w, 400, "更换 OpenClash 地址时请重新输入密钥")
				return
			}
			c.ClashSecret = old.ClashSecret
		}
		if c.ROS.Password == "" && !in.ClearROSPassword {
			if old.ROS.Host != "" && (c.ROS.Host != old.ROS.Host || c.ROS.Port != old.ROS.Port || c.ROS.Username != old.ROS.Username || c.ROS.TLS != old.ROS.TLS) && old.ROS.Password != "" {
				apiError(w, 400, "更换 ROS 连接时请重新输入密码")
				return
			}
			c.ROS.Password = old.ROS.Password
		}
		if e = c.Validate(); e != nil {
			apiError(w, 400, e.Error())
			return
		}
		if e = s.manager.Save(c); e != nil {
			apiError(w, 500, "保存失败，请检查统计数据卷")
			return
		}
		jsonOut(w, 200, c.Public())
	default:
		apiError(w, 404, "未找到扩展接口")
	}
}
func period(q url.Values, now time.Time) (int64, int64, error) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	if loc == nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	n := now.In(loc)
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
	from, to := today, today.AddDate(0, 0, 1)
	switch q.Get("period") {
	case "", "today":
	case "yesterday":
		from = today.AddDate(0, 0, -1)
		to = today
	case "month":
		from = time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, loc)
	case "all":
		from = today.AddDate(0, 0, -364)
	case "custom":
		var e error
		from, e = time.ParseInLocation("2006-01-02", q.Get("from"), loc)
		if e != nil {
			return 0, 0, errors.New("开始日期格式错误")
		}
		to, e = time.ParseInLocation("2006-01-02", q.Get("to"), loc)
		if e != nil {
			return 0, 0, errors.New("结束日期格式错误")
		}
		to = to.AddDate(0, 0, 1)
	default:
		return 0, 0, errors.New("未知统计周期")
	}
	if !to.After(from) || to.Sub(from) > 366*24*time.Hour || to.After(today.AddDate(0, 0, 1)) {
		return 0, 0, errors.New("日期范围须为过去至今日，最长 366 天")
	}
	return from.UnixMilli(), to.UnixMilli(), nil
}
