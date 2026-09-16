package traffic

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"mikrodash/internal/routeros"
)

type Rate struct {
	At   int64 `json:"at"`
	Down int64 `json:"down"`
	Up   int64 `json:"up"`
}
type Health struct {
	LastAt int64  `json:"lastAt"`
	Error  string `json:"error"`
	Count  int    `json:"count"`
}
type Live struct {
	Clash    Health `json:"clash"`
	ROS      Health `json:"ros"`
	Down     int64  `json:"down"`
	Up       int64  `json:"up"`
	Download int64  `json:"download"`
	Upload   int64  `json:"upload"`
	Rates    []Rate `json:"rates"`
}
type worker struct {
	cancel context.CancelFunc
	done   chan struct{}
}
type Manager struct {
	db      *DB
	mu      sync.RWMutex
	change  sync.Mutex
	configs map[string]Config
	live    map[string]Live
	workers map[string]worker
	client  *http.Client
	ctx     context.Context
}

func newHTTPClient() *http.Client {
	return &http.Client{Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, ResponseHeaderTimeout: 5 * time.Second, IdleConnTimeout: 30 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func NewManager(ctx context.Context, d *DB) (*Manager, error) {
	m := &Manager{db: d, ctx: ctx, configs: map[string]Config{}, live: map[string]Live{}, workers: map[string]worker{}, client: newHTTPClient()}
	cfgs, e := d.Configs()
	if e != nil {
		return nil, e
	}
	for _, c := range cfgs {
		if e = c.Validate(); e != nil {
			return nil, e
		}
		m.configs[c.RouterID] = c
	}
	for _, c := range cfgs {
		m.start(c)
	}
	return m, nil
}
func (m *Manager) Config(id string) Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.configs[id]
	if !ok {
		return Config{RouterID: id, ROS: ROSConfig{Port: 8728}, Infrastructure: []string{}}
	}
	return c
}
func (m *Manager) Snapshot(id string) Live {
	m.mu.RLock()
	defer m.mu.RUnlock()
	x := m.live[id]
	x.Rates = append([]Rate{}, x.Rates...)
	return x
}
func (m *Manager) mutate(id string, f func(*Live)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := m.live[id]
	f(&v)
	m.live[id] = v
}
func (m *Manager) Save(c Config) error {
	m.change.Lock()
	defer m.change.Unlock()
	if e := c.Validate(); e != nil {
		return e
	}
	if e := m.db.SaveConfig(c); e != nil {
		return e
	}
	m.stop(c.RouterID)
	m.mu.Lock()
	m.configs[c.RouterID] = c
	delete(m.live, c.RouterID)
	m.mu.Unlock()
	m.start(c)
	return nil
}
func (m *Manager) stop(id string) {
	if w, ok := m.workers[id]; ok {
		w.cancel()
		<-w.done
		delete(m.workers, id)
	}
}
func (m *Manager) Close() {
	m.change.Lock()
	defer m.change.Unlock()
	for id := range m.workers {
		m.stop(id)
	}
	m.client.CloseIdleConnections()
}
func (m *Manager) start(c Config) {
	if !c.Enabled {
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	done := make(chan struct{})
	m.workers[c.RouterID] = worker{cancel, done}
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		if c.ClashURL != "" {
			wg.Add(1)
			go func() { defer wg.Done(); m.clashLoop(ctx, c) }()
		}
		if c.ROS.Host != "" {
			wg.Add(1)
			go func() { defer wg.Done(); m.rosLoop(ctx, c) }()
		}
		wg.Wait()
	}()
}
func epoch() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func pause(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (m *Manager) clashLoop(ctx context.Context, c Config) {
	for ctx.Err() == nil {
		e := m.clashStream(ctx, c)
		if ctx.Err() != nil {
			return
		}
		if e != nil {
			m.mutate(c.RouterID, func(v *Live) {
				v.Clash.Error = "OpenClash 采集断开：请检查地址、密钥、核心状态或统计磁盘"
			})
		}
		if !pause(ctx, 5*time.Second) {
			return
		}
	}
}
func (m *Manager) clashStream(parent context.Context, c Config) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, c.ClashURL+"/traffic", nil)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+c.ClashSecret)
	resp, e := m.client.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("controller rejected request")
	}
	heartbeat := make(chan struct{}, 1)
	go func() {
		t := time.NewTimer(6 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				cancel()
				return
			case <-heartbeat:
				t.Reset(6 * time.Second)
			}
		}
	}()
	scan := bufio.NewScanner(resp.Body)
	scan.Buffer(make([]byte, 4096), 65536)
	run := epoch()
	lastCount := time.Time{}
	for scan.Scan() {
		select {
		case heartbeat <- struct{}{}:
		default:
		}
		var f struct {
			Up, Down  int64
			UpTotal   *int64 `json:"upTotal"`
			DownTotal *int64 `json:"downTotal"`
		}
		if e = json.Unmarshal(scan.Bytes(), &f); e != nil {
			return e
		}
		if f.UpTotal == nil || f.DownTotal == nil || f.Up < 0 || f.Down < 0 {
			return errors.New("unsupported traffic fields")
		}
		at := time.Now().UnixMilli()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		e = m.db.Record(Sample{Router: c.RouterID, Kind: "clash", Device: "core", Epoch: run, At: at, Down: *f.DownTotal, Up: *f.UpTotal}, 5*time.Second)
		if e != nil {
			return e
		}
		m.mutate(c.RouterID, func(v *Live) {
			v.Clash.LastAt = at
			v.Clash.Error = ""
			v.Down = f.Down
			v.Up = f.Up
			v.Download = *f.DownTotal
			v.Upload = *f.UpTotal
			v.Rates = append(v.Rates, Rate{at, f.Down, f.Up})
			if len(v.Rates) > 120 {
				v.Rates = v.Rates[len(v.Rates)-120:]
			}
		})
		// Counts are optional; failures must not interrupt the authoritative counters.
		if time.Since(lastCount) > 15*time.Second {
			lastCount = time.Now()
			go m.connectionCount(ctx, c)
		}
	}
	if e = scan.Err(); e != nil {
		return e
	}
	return io.EOF
}
func (m *Manager) connectionCount(parent context.Context, c Config) {
	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, c.ClashURL+"/connections", nil)
	if e != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.ClashSecret)
	resp, e := m.client.Do(req)
	if e != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return
	}
	var x struct {
		Connections []json.RawMessage `json:"connections"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&x) != nil || ctx.Err() != nil {
		return
	}
	m.mutate(c.RouterID, func(v *Live) { v.Clash.Count = len(x.Connections) })
}

func (m *Manager) rosLoop(ctx context.Context, c Config) {
	for ctx.Err() == nil {
		err := m.rosSession(ctx, c)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			m.mutate(c.RouterID, func(v *Live) {
				v.ROS.Error = "ROS 采集失败：请检查 API、账号、Kid Control 统计或统计磁盘"
			})
		}
		if !pause(ctx, 10*time.Second) {
			return
		}
	}
}

type rosReader interface {
	Do(routeros.Cmd) ([]routeros.Reply, error)
}

func readROS(r rosReader) ([]Sample, error) {
	read := func(path string, args ...string) ([]routeros.Reply, error) {
		return r.Do(routeros.Cmd{Path: path, Args: args, Timeout: 5 * time.Second})
	}
	base, e := read("/ip/kid-control/device/print", "=.proplist=.id,name,mac-address")
	if e != nil {
		return nil, e
	}
	stats, e := read("/ip/kid-control/device/print", "=stats=", "=.proplist=.id,bytes-down,bytes-up,rate-down,rate-up")
	if e != nil {
		return nil, e
	}
	arp, _ := read("/ip/arp/print", "=.proplist=address,mac-address")
	ids := map[string]routeros.Reply{}
	ips := map[string]string{}
	for _, r := range base {
		ids[r[".id"]] = r
	}
	for _, r := range arp {
		if mac, e := net.ParseMAC(r["mac-address"]); e == nil {
			key := strings.ToUpper(mac.String())
			if old := ips[key]; old != "" && old != r["address"] {
				ips[key] = "多个地址"
			} else {
				ips[key] = r["address"]
			}
		}
	}
	byMAC := map[string]Sample{}
	for _, r := range stats {
		b := ids[r[".id"]]
		mac, e := net.ParseMAC(b["mac-address"])
		if e != nil {
			continue
		}
		down, ed := strconv.ParseInt(r["bytes-down"], 10, 64)
		up, eu := strconv.ParseInt(r["bytes-up"], 10, 64)
		if ed != nil || eu != nil || down < 0 || up < 0 {
			return nil, errors.New("missing cumulative device counters")
		}
		key := strings.ToUpper(mac.String())
		// Duplicate MAC rows must not be added together; ambiguous identities are omitted.
		if _, exists := byMAC[key]; exists {
			return nil, errors.New("duplicate kid-control MAC")
		}
		byMAC[key] = Sample{Device: key, Name: b["name"], IP: ips[key], Down: down, Up: up, Epoch: r[".id"]}
	}
	out := make([]Sample, 0, len(byMAC))
	for _, s := range byMAC {
		out = append(out, s)
	}
	return out, nil
}
func (m *Manager) rosSession(ctx context.Context, c Config) error {
	r, e := routeros.Dial(routeros.Config{Host: c.ROS.Host, Port: c.ROS.Port, Username: c.ROS.Username, Password: c.ROS.Password, TLS: c.ROS.TLS, InsecureTLS: c.ROS.InsecureTLS, DialTimeout: 5 * time.Second})
	if e != nil {
		return e
	}
	defer r.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			r.Close()
		case <-done:
		}
	}()
	run := epoch()
	for ctx.Err() == nil {
		rows, e := readROS(r)
		if e != nil {
			return e
		}
		at := time.Now().UnixMilli()
		for _, s := range rows {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.Router = c.RouterID
			s.Kind = "device"
			s.At = at
			s.Epoch = run + ":" + s.Epoch
			if e = m.db.Record(s, 45*time.Second); e != nil {
				return e
			}
		}
		m.mutate(c.RouterID, func(v *Live) { v.ROS.LastAt = at; v.ROS.Error = ""; v.ROS.Count = len(rows) })
		if !pause(ctx, 10*time.Second) {
			return ctx.Err()
		}
	}
	return ctx.Err()
}
