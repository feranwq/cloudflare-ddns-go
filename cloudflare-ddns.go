package main

import (
	"encoding/json"
	"fmt"
	"os"
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

func GetIP() (ips []IP) {
	ipv4Enabled := cfg.MustGet(ctx, "a")
	ipv6Enabled := cfg.MustGet(ctx, "aaaa")
	ipv4Url := "https://4.ipw.cn" // "https://1.1.1.1/cdn-cgi/trace"
	ipv6Url := "https://6.ipw.cn" // "https://[2606:4700:4700::1111]/cdn-cgi/trace"
	if ipv4Enabled.Bool() {
		ip, err := getIP(ipv4Url)
		ip.IPType = "A"
		if err != nil {
			fmt.Println("Failed to get ipv4 address")
		} else {
			fmt.Printf("🧩 IPv4 %s detected\n", ip.IPAddress)
		}
		ips = append(ips, ip)
	}
	if ipv6Enabled.Bool() {
		ip, err := getIP(ipv6Url)
		ip.IPType = "AAAA"
		if err != nil {
			fmt.Println("Failed to get ipv6 address")
		} else {
			fmt.Printf("🧩 IPv6 %s detected\n", ip.IPAddress)
		}
		ips = append(ips, ip)
	}
	return
}

func getIP(url string) (ip IP, err error) {
	r, err := client.Get(ctx, url)
	if err != nil {
		return ip, err
	}
	defer r.Close()
	ip.IPAddress = r.ReadAllString()
	return ip, nil
}

func CommitRecord(ip IP) {
	url := "https://api.cloudflare.com/client/v4/"
	cloudflareCfgs := &CloudflareCfgs{}
	if err := cfg.MustGet(ctx, "cloudflare").Struct(cloudflareCfgs); err != nil {
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
				TTL:     cfg.MustGet(ctx, "ttl").Int(),
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
	client.SetTimeout(cfg.MustGet(ctx, "timeout").Duration())
	fmt.Printf("🕰️ Updating records every %s...\n", cfg.MustGet(ctx, "repeat").String())
	ips := GetIP()
	for _, ip := range ips {
		CommitRecord(ip)
	}
	for {
		select {
		case <-c:
			fmt.Println("🛑 Stopping main thread...")
			return
		case <-time.After(time.Duration(cfg.MustGet(ctx, "repeat").Duration())):
			ips := GetIP()
			for _, ip := range ips {
				CommitRecord(ip)
			}
		}
	}
}
