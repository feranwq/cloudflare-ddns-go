package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/gogf/gf/v2/net/gclient"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/gctx"
)

var (
	ctx    = gctx.New()
	cfg    = gcfg.Instance()
	client = gclient.New()
)

type IP struct {
	IPType    string `json:"type"`
	IPAddress string `json:"address"`
}

type CloudflareCfgs []CloudflareCfg

type CloudflareCfg struct {
	Authentication CloudflareAuth `json:"authentication"`
	ZoneID         string         `json:"zone_id"`
	Subdomains     []Subdomain    `json:"subdomains"`
}

type CloudflareAuth struct {
	APIToken     string `json:"api_token"`
	APIKey       string `json:"api_key"`
	AccountEmail string `json:"account_email"`
}

type Subdomain struct {
	Name    string `json:"name"`
	Proxied bool   `json:"proxied"`
}

type Record struct {
	IPType  string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl"`
}

type ZoneResp struct {
	Result ZoneResult `json:"result"`
}

type ZoneResult struct {
	Name string `json:"name"`
}

type RecordResp struct {
	Result []RecordResult `json:"result"`
}

type RecordResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

func GetIP(url, testIP, ipType string, warp bool, ) (ip IP, err error) {
	ip.IPType = ipType
	if warp {
		ip, err = getLocalIP(testIP)
		if err != nil {
			fmt.Println("Failed to get address")
			return
		}
		return
	}
	ip, err = getIP(url)
	if err != nil {
		fmt.Println("Failed to get address")
		return
	}
	return
}

func getIP(url string) (ip IP, err error) {
	r, err := client.Get(ctx, url)
	if err != nil {
		return
	}
	defer r.Close()
	ip.IPAddress = r.ReadAllString()
	fmt.Printf("🧩 IP %s detected\n", ip.IPAddress)
	return ip, nil
}

func getLocalIP(testIP string) (ip IP, err error) {
	// testIP := "2606:4700:4700::1001"

	cmd := exec.Command("sh", "-c", fmt.Sprintf(`ip route get %s 2>/dev/null | grep -oP 'src \K\S+'`, testIP))
	output, err := cmd.Output()
	if err != nil {
		return
	}
	ip.IPAddress = string(output)
	fmt.Printf("🧩 LocalIP %s detected\n", ip.IPAddress)
	return ip, nil
}


func CommitRecord(ip IP) {
	url := "https://api.cloudflare.com/client/v4/"
	cloudflareCfgs := &CloudflareCfgs{}
	if err := cfg.MustGetWithEnv(ctx, "cloudflare").Struct(cloudflareCfgs); err != nil {
		fmt.Printf("😡 cloudflare config load failed, error msg: %s\n", err)
		return
	}
	for _, option := range *cloudflareCfgs {
		zoneResp := &ZoneResp{}
		recordResp := &RecordResp{}
		client.SetHeader("Authorization", "Bearer "+option.Authentication.APIToken)
		defer client.SetHeader("Authorization", "")
		r, err := client.Get(ctx, url+"zones/"+option.ZoneID)
		if err != nil {
			fmt.Printf("😡 get cloudflare zone info failed, error msg: %s\n", err)
			return
		}
		defer r.Close()
		err = json.Unmarshal([]byte(r.ReadAllString()), zoneResp)
		if err != nil {
			fmt.Printf("😡 get cloudflare base domain info failed, error msg: %s\n", err)
			return
		}
		baseDomain := zoneResp.Result.Name

		r, err = client.Get(ctx, url+"zones/"+option.ZoneID+"/dns_records?per_page=100&type="+ip.IPType)
		if err != nil {
			fmt.Printf("😡 get cloudflare dns_records info failed, error msg: %s\n", err)
			return
		}
		defer r.Close()
		err = json.Unmarshal([]byte(r.ReadAllString()), recordResp)
		if err != nil {
			fmt.Printf("😡 get cloudflare dns_records domain info failed, error msg: %s\n", err)
			return
		}

		for _, subDomain := range option.Subdomains {
			insert := true
			update := false
			identifier := ""
			fqdn := fmt.Sprintf("%s.%s", subDomain.Name, baseDomain)
			record := &Record{
				IPType:  ip.IPType,
				Name:    fqdn,
				Content: ip.IPAddress,
				Proxied: subDomain.Proxied,
				TTL:     cfg.MustGetWithEnv(ctx, "ttl").Int(),
			}
			recordJson, _ := json.Marshal(record)
			for _, domain := range recordResp.Result {
				if fqdn == domain.Name {
					insert = false
				}
				if fqdn == domain.Name && ip.IPAddress != domain.Content {
					update = true
					identifier = domain.ID
				}
			}
			switch {
			case insert:
				r, err = client.Post(ctx, url+"zones/"+option.ZoneID+"/dns_records", recordJson)
				if err != nil {
					fmt.Printf("😡 insert cloudflare dns_records info failed, error msg: %s\n", err)
					return
				}
				defer r.Close()
				fmt.Printf("➕ insert cloudflare %s %s...\n", fqdn, ip.IPAddress)
			case update:
				r, err = client.Put(ctx, url+"zones/"+option.ZoneID+"/dns_records/"+identifier, recordJson)
				if err != nil {
					fmt.Printf("😡 update cloudflare dns_records info failed, error msg: %s\n", err)
					return
				}
				defer r.Close()
				fmt.Printf("📡 update cloudflare %s %s...\n", fqdn, ip.IPAddress)
			}
		}
	}
}

func main() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	client.SetTimeout(cfg.MustGetWithEnv(ctx, "timeout").Duration())
	fmt.Printf("🕰️ Updating records every %s...\n", cfg.MustGetWithEnv(ctx, "repeat").String())
	ipv4Enabled := cfg.MustGetWithEnv(ctx, "a")
	ipv6Enabled := cfg.MustGetWithEnv(ctx, "aaaa")
	ipv4Url := "https://4.ipw.cn" // "https://1.1.1.1/cdn-cgi/trace"
	ipv6Url := "https://6.ipw.cn" // "https://[2606:4700:4700::1111]/cdn-cgi/trace"
	ipv4TestIP := "1.1.1.1"
	ipv6TestIP := "2606:4700:4700::1111"
	var ips []IP
	if ipv4Enabled.Bool() {
		ip, _ := GetIP(ipv4Url, ipv4TestIP, "A", false)
		ips = append(ips, ip)
	}
	if ipv6Enabled.Bool() {
		ip, _ := GetIP(ipv6Url, ipv6TestIP, "AAAA", true)
		ips = append(ips, ip)
	}
	for _, ip := range ips {
		CommitRecord(ip)
	}
	for {
		select {
		case <-c:
			fmt.Println("🛑 Stopping main thread...")
			return
		case <-time.After(time.Duration(cfg.MustGetWithEnv(ctx, "repeat").Duration())):
			ips = []IP{}
			if ipv4Enabled.Bool() {
				ip, _ := GetIP(ipv4Url, ipv4TestIP, "A", false)
				ips = append(ips, ip)
			}
			if ipv6Enabled.Bool() {
				ip, _ := GetIP(ipv6Url, ipv6TestIP, "AAAA", true)
				ips = append(ips, ip)
			}
			for _, ip := range ips {
				CommitRecord(ip)
			}
		}
	}
}
