// -*- coding: utf-8 -*-
package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/websocket"
)

// 接続先情報
type HostInfo struct {
	// スキーム。 http:// など
	Scheme string
	// ホスト名
	Name string
	// ポート番号
	Port int
	// パス
	Path string
}

// 接続先の文字列表現
func (info *HostInfo) toStr() string {
	return fmt.Sprintf("%s%s:%d%s", info.Scheme, info.Name, info.Port, info.Path)
}

// tunnel の制御パラメータ
type TunnelParam struct {
	// 接続可能な IP パターン。
	// nil の場合、 IP 制限しない。
	maskedIP *MaskIP
	// サーバ情報
	wsServerInfo HostInfo
	// サーバ情報
	tcpServerInfo HostInfo
}

func listenTcpServer(
	local net.Listener, param *TunnelParam, process func(connInfo io.ReadWriteCloser)) {
	conn, err := local.Accept()
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	remoteAddr := fmt.Sprintf("%s", conn.RemoteAddr())
	log.Print("connected -- ", remoteAddr)
	if err := AcceptClient(remoteAddr, param); err != nil {
		log.Printf("unmatch ip -- %s", remoteAddr)
		time.Sleep(3 * time.Second)
		return
	}
	defer ReleaseClient(remoteAddr)

	process(conn)
}

func StartServer(
	param *TunnelParam, tcpConnChan chan io.ReadWriteCloser, synChan chan bool) {
	log.Print("waiting --- ", param.tcpServerInfo.toStr())
	local, err := net.Listen("tcp", param.tcpServerInfo.toStr())
	if err != nil {
		log.Fatal(err)
	}
	defer local.Close()

	for {
		listenTcpServer(local, param,
			func(connInfo io.ReadWriteCloser) {
				tcpConnChan <- connInfo
				<-synChan
			})
	}
}

type WrapWSHandler struct {
	handle func(ws *websocket.Conn, remoteAddr string)
	param  *TunnelParam
}

func (handler WrapWSHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {

	if err := AcceptClient(req.RemoteAddr, handler.param); err != nil {
		log.Printf("reject -- %s", err)
		w.WriteHeader(http.StatusNotAcceptable)
		//fmt.Fprintf( w, "%v\n", err )
		time.Sleep(3 * time.Second)
		return
	}
	defer ReleaseClient(req.RemoteAddr)

	wrap := func(ws *websocket.Conn) {
		handler.handle(ws, req.RemoteAddr)
	}

	wshandler := websocket.Handler(wrap)
	wshandler.ServeHTTP(w, req)
}

func execWebSocketServer(
	param TunnelParam,
	connectSession func(conn io.ReadWriteCloser, param *TunnelParam)) {
	handle := func(ws *websocket.Conn, remoteAddr string) {
		connectSession(ws, &param)
	}

	wrapHandler := WrapWSHandler{handle, &param}

	http.Handle("/", wrapHandler)
	err := http.ListenAndServe(param.wsServerInfo.toStr(), nil)
	if err != nil {
		panic("ListenAndServe: " + err.Error())
	}
}

func StartWebsocketServer(
	param *TunnelParam, wsConnChan chan io.ReadWriteCloser, synChan chan bool) {
	log.Print("start websocket -- ", param.wsServerInfo.toStr())

	execWebSocketServer(
		*param,
		func(connInfo io.ReadWriteCloser, tunnelParam *TunnelParam) {
			wsConnChan <- connInfo
			<-synChan
		})
}

type LinkParam struct {
	wsConnChan  chan io.ReadWriteCloser
	tcpConnChan chan io.ReadWriteCloser
	linkChan    chan bool
}

func TransProc(reader io.Reader, writer io.Writer, syncChan chan bool) {
	io.Copy(writer, reader)
	syncChan <- true
}

func LinkProc(linkParam *LinkParam) {
	syncChan := make(chan bool, 2)
	for {
		wsConn := <-linkParam.wsConnChan
		tcpConn := <-linkParam.tcpConnChan

		log.Printf("link -- start")

		go TransProc(wsConn, tcpConn, syncChan)
		go TransProc(tcpConn, wsConn, syncChan)

		<-syncChan
		<-syncChan

		linkParam.linkChan <- true
		linkParam.linkChan <- true

		log.Printf("link -- end")
	}
}
