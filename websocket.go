// -*- coding: utf-8 -*-
package main

import (
	"io"
	"os"
	"strings"

	"crypto/tls"
	"log"

	"golang.org/x/net/websocket"
)

// websocketUrl で示すサーバに websocket で接続する
func ConnectWebScoket(websocketUrl string) {

	log.Printf("%s", websocketUrl)

	conf, err := websocket.NewConfig(websocketUrl, "http://localhost")
	if err != nil {
		log.Fatal("NewConfig error", err)
	}
	if strings.Index(websocketUrl, "wss") == 0 {
		// とりあえず tls の検証を skip する
		conf.TlsConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}
	var websock *websocket.Conn
	websock, err = websocket.DialConfig(conf)
	if err != nil {
		log.Fatal("websocket error", err)
	}

	go io.Copy(websock, os.Stdin)
	io.Copy(os.Stdout, websock)
}
