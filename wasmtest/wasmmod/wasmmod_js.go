// Copyright (c) 2021 Tailscale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The wasmmod is a Tailscale-in-wasm proof of concept.
//
// See ../index.html and ../term.js for how it ties together.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"sync"
	"syscall/js"
	"time"

	"github.com/skip2/go-qrcode"
	"golang.org/x/crypto/ssh"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnserver"
	"tailscale.com/net/netns"
	"tailscale.com/net/packet"
	"tailscale.com/net/socks5/tssocks"
	"tailscale.com/net/tstun"
	"tailscale.com/types/logger"
	"tailscale.com/wgengine"
	"tailscale.com/wgengine/filter"
	"tailscale.com/wgengine/netstack"
)

func main() {
	var mut sync.Mutex

	inputChan := make(chan []byte, 10)

	netns.SetEnabled(false)
	var logf logger.Logf = log.Printf
	eng, err := wgengine.NewUserspaceEngine(logf, wgengine.Config{})
	if err != nil {
		log.Fatal(err)
	}
	tunDev, magicConn, ok := eng.(wgengine.InternalsGetter).GetInternals()
	if !ok {
		log.Fatalf("%T is not a wgengine.InternalsGetter", eng)
	}
	ns, err := netstack.Create(logf, tunDev, eng, magicConn)
	if err != nil {
		log.Fatalf("netstack.Create: %v", err)
	}
	ns.ProcessLocalIPs = true
	ns.ProcessSubnets = true
	if err := ns.Start(); err != nil {
		log.Fatalf("failed to start netstack: %v", err)
	}

	doc := js.Global().Get("document")
	topBar := doc.Call("getElementById", "topbar")
	topBarStyle := topBar.Get("style")
	netmapEle := doc.Call("getElementById", "netmap")
	loginEle := doc.Call("getElementById", "loginURL")

	netstackHandlePacket := tunDev.PostFilterIn
	tunDev.PostFilterIn = func(p *packet.Parsed, t *tstun.Wrapper) filter.Response {
		if p.IsEchoRequest() {
			go func() {
				topBarStyle.Set("background", "gray")
				time.Sleep(100 * time.Millisecond)
				topBarStyle.Set("background", "white")
			}()
		}
		return netstackHandlePacket(p, t)
	}

	socksSrv := tssocks.NewServer(logger.WithPrefix(logf, "socks5: "), eng, ns)

	var store ipn.StateStore = new(ipn.MemoryStore)
	srv, err := ipnserver.New(log.Printf, "some-logid", store, eng, nil, ipnserver.Options{
		SurviveDisconnects: true,
	})
	if err != nil {
		log.Fatalf("ipnserver.New: %v", err)
	}
	lb := srv.LocalBackend()

	lb.SetNotifyCallback(func(n ipn.Notify) {
		log.Printf("NOTIFY: %+v", n)
		if n.LoginFinished != nil {
			loginEle.Set("innerHTML", "")
			topBar.Set("innerHTML", "<div>Step 3: Connect to ssh!</div>")
		}

		if nm := n.NetMap; nm != nil {
			var buf bytes.Buffer
			fmt.Fprintf(&buf, "<h2>Tailscale info</h2><input type=button value='Logout' onclick='logoutClicked()'>")
			fmt.Fprintf(&buf, "<p>Name: <b>%s</b></p>", html.EscapeString(nm.Name))
			fmt.Fprintf(&buf, "<p>Addresses: ")
			for i, a := range nm.Addresses {
				if i == 0 {
					fmt.Fprintf(&buf, "<b>%s</b>", a.IP())
				} else {
					fmt.Fprintf(&buf, ", %s", a.IP())
				}
			}
			fmt.Fprintf(&buf, "</p>")
			fmt.Fprintf(&buf, "<p>Machine: <b>%v</b>, %v</p>", nm.MachineStatus, nm.MachineKey)
			fmt.Fprintf(&buf, "<p>Nodekey: %v</p>", nm.NodeKey)
			fmt.Fprintf(&buf, "<hr><table>")
			for _, p := range nm.Peers {
				var ip string
				if len(p.Addresses) > 0 {
					ip = p.Addresses[0].IP().String()
				}
				fmt.Fprintf(&buf, `<tr><td>%s</td><td>%s</td>`, ip, html.EscapeString(p.Name))
				fmt.Fprintf(&buf, `<td><button onclick="runSSH('%s')">Connect to port 2200</button></td></tr>`, ip)

			}
			fmt.Fprintf(&buf, "</table>")
			netmapEle.Set("innerHTML", buf.String())
		}
		if n.BrowseToURL != nil {
			esc := html.EscapeString(*n.BrowseToURL)
			pngBytes, _ := qrcode.Encode(*n.BrowseToURL, qrcode.Medium, 256)
			qrDataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
			loginEle.Set("innerHTML", fmt.Sprintf("<a href='%s' target=_blank><br/><img src='%s' border=0></a>", esc, qrDataURL))
		}
	})

	start := func() {
		err := lb.Start(ipn.Options{
			Prefs: &ipn.Prefs{
				ControlURL:       "https://controlplane.tailscale.com",
				RouteAll:         false,
				AllowSingleHosts: true,
				WantRunning:      true,
				Hostname:         "wasm",
			},
		})
		log.Printf("Start error: %v", err)
		lb.StartLoginInteractive()
	}
	go start()

	js.Global().Set("logoutClicked", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		log.Printf("Logout clicked")
		if lb.State() == ipn.NoState {
			log.Printf("Backend not running")
			return nil
		}
		go func() {
			lb.Logout()
			js.Global().Get("location").Call("reload") // There's probably a better way to get rid of terminal
		}()
		return nil
	}))

	js.Global().Set("runSSH", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) < 1 {
			log.Printf("missing args")
			return nil
		}
		go func() {
			mut.Lock()
			defer mut.Unlock()

			js.Global().Get("startTerminal").Invoke()

			host := args[0].String()

			term := js.Global().Get("theTerminal")

			term.Call("write", fmt.Sprintf("Connecting to %s:2200\r\n", host))

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			c, err := socksSrv.Dialer(ctx, "tcp", net.JoinHostPort(host, "2200"))
			if err != nil {
				term.Call("write", fmt.Sprintf("Error establishing session: %v\r\n", err))
				term.Call("write", "Try again or disconnect/reconnect to tailscale")
				return
			}
			term.Call("write", "TCP Session Established\r\n")
			defer c.Close()

			config := &ssh.ClientConfig{
				HostKeyCallback: ssh.InsecureIgnoreHostKey(),
				User:            "user",
				Auth: []ssh.AuthMethod{
					ssh.Password("1234"),
				},
			}

			sshConn, _, _, err := ssh.NewClientConn(c, host, config)
			if err != nil {
				term.Call("write", fmt.Sprintf("Error: %v\r\n", err))
				return
			}
			defer sshConn.Close()
			term.Call("write", "SSH Connected\r\n")

			sshClient := ssh.NewClient(sshConn, nil, nil)
			defer sshClient.Close()

			session, err := sshClient.NewSession()
			if err != nil {
				log.Println("Session Failed")
				term.Call("write", fmt.Sprintf("Error: %v\r\n", err))
				return
			}
			term.Call("write", "Session Established\r\n")
			defer session.Close()

			stdin, err := session.StdinPipe()
			if err != nil {
				fmt.Println(err.Error())
			}

			stdout, err := session.StdoutPipe()
			if err != nil {
				fmt.Println(err.Error())
			}

			stderr, err := session.StderrPipe()
			if err != nil {
				fmt.Println(err.Error())
			}

			time.Sleep(1 * time.Second)
			term.Call("clear", "")

			term.Set("_inSSH", true)
			defer func() {
				term.Set("_inSSH", false)
			}()

			done := make(chan bool, 1)

			// Input
			go func() {
				for {
					select {
					case d := <-inputChan:
						_, err := stdin.Write(d)
						if err != nil {
							term.Call("write", fmt.Sprintf("Error: %v\r\n", err))
						}
					case <-done:
						log.Printf("Closing reader")
						return
					}
				}
			}()

			var wg sync.WaitGroup

			// Output + Err
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := io.Copy(termWriter{term}, stdout)
				if err != nil {
					term.Call("write", fmt.Sprintf("Error: %v\r\n", err))
					return
				}
			}()

			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := io.Copy(termWriter{term}, stderr)
				if err != nil {
					term.Call("write", fmt.Sprintf("Error: %v\r\n", err))
					return
				}
			}()

			err = session.RequestPty("xterm", term.Get("rows").Int(), term.Get("cols").Int(), ssh.TerminalModes{})

			if err != nil {
				log.Fatal("request for pseudo terminal failed: ", err)
				term.Call("write", fmt.Sprintf("Error: %v\r\n", err))
				return
			}

			err = session.Shell()
			if err != nil {
				log.Println("Session Failed")
				term.Call("write", fmt.Sprintf("Error: %v\r\n", err))
				return
			}
			log.Println("Shell started")

			wg.Wait()
			log.Printf("Closed connection")
			// Close input
			done <- true
		}()
		return nil
	}))

	js.Global().Set("sendSSHInput", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		input := args[0].String()
		inputChan <- []byte(input)
		return nil
	}))

	select {}
}

type termWriter struct {
	o js.Value
}

func (w termWriter) Write(p []byte) (n int, err error) {
	r := bytes.Replace(p, []byte("\n"), []byte("\n\r"), -1)
	w.o.Call("write", string(r))
	return len(p), nil
}
