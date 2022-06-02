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

type LinkPort struct {
	// websocket サーバ情報
	wsServerInfo HostInfo
	// tcp サーバ情報
	tcpServerInfo HostInfo
}

// tunnel の制御パラメータ
type TunnelParam struct {
	// 接続可能な IP パターン。
	// nil の場合、 IP 制限しない。
	maskedIP   *MaskIP
	maxSession int
}

func listenTcpServer(
	local net.Listener, param *TunnelParam, tcpServerInfo HostInfo,
	process func(connInfo io.ReadWriteCloser)) {
	conn, err := local.Accept()
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	remoteAddr := fmt.Sprintf("%s", conn.RemoteAddr())
	log.Print("connected -- ", remoteAddr)
	if err := AcceptClient(remoteAddr, param); err != nil {
		log.Printf("%v", err)
		time.Sleep(3 * time.Second)
		return
	}
	defer ReleaseClient(remoteAddr)

	process(conn)
}

func StartServer(
	param *TunnelParam, tcpServerInfo HostInfo, linkDataChan *LinkDataChan) {
	log.Print("waiting --- ", tcpServerInfo.toStr())
	local, err := net.Listen("tcp", tcpServerInfo.toStr())
	if err != nil {
		log.Fatal(err)
	}
	defer local.Close()

	for {
		listenTcpServer(local, param, tcpServerInfo,
			func(connInfo io.ReadWriteCloser) {
				linkDataChan.connChan <- connInfo
				<-linkDataChan.endChan
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
	param TunnelParam, wsServerInfo HostInfo,
	connectSession func(conn io.ReadWriteCloser, param *TunnelParam)) {
	handle := func(ws *websocket.Conn, remoteAddr string) {
		connectSession(ws, &param)
	}

	wrapHandler := WrapWSHandler{handle, &param}

	var srv http.Server
	srv.Handler = wrapHandler
	srv.Addr = wsServerInfo.toStr()
	err := srv.ListenAndServe()
	if err != nil {
		panic("ListenAndServe: " + err.Error())
	}

	// http.Handle("/", wrapHandler)
	// err := http.ListenAndServe(wsServerInfo.toStr(), nil)
	// if err != nil {
	// 	panic("ListenAndServe: " + err.Error())
	// }
}

func StartWebsocketServer(
	param *TunnelParam, wsServerInfo HostInfo, linkDataChan *LinkDataChan) {
	log.Print("start websocket -- ", wsServerInfo.toStr())

	execWebSocketServer(
		*param, wsServerInfo,
		func(connInfo io.ReadWriteCloser, tunnelParam *TunnelParam) {
			linkDataChan.connChan <- connInfo
			<-linkDataChan.endChan
		})
}

type LinkDataChan struct {
	connChan      chan io.ReadWriteCloser
	writerChan    chan io.Writer
	fromDataChan  chan LinkBuf
	toDataChan    chan LinkBuf
	endChan       chan io.ReadWriteCloser
	currentWriter io.Writer
}

type LinkBuf struct {
	buf []byte
	len int
}

type LinkParam struct {
	wsLinkDataChan  LinkDataChan
	tcpLinkDataChan LinkDataChan
}

func createLinkBuf() LinkBuf {
	size := 64 * 1024
	return LinkBuf{
		buf: make([]byte, size),
		len: size,
	}
}

func createLinkDataChan() LinkDataChan {
	linkDataChan := LinkDataChan{
		connChan:      make(chan io.ReadWriteCloser, 1),
		writerChan:    make(chan io.Writer, 2),
		fromDataChan:  make(chan LinkBuf, 2),
		toDataChan:    make(chan LinkBuf, 2),
		endChan:       make(chan io.ReadWriteCloser, 1),
		currentWriter: nil,
	}
	// websocket の 1 フレームの最大 64KB 分のバッファを登録
	linkDataChan.fromDataChan <- createLinkBuf()
	linkDataChan.fromDataChan <- createLinkBuf()
	return linkDataChan
}

func CreateLinkParam() *LinkParam {
	return &LinkParam{
		wsLinkDataChan:  createLinkDataChan(),
		tcpLinkDataChan: createLinkDataChan(),
	}
}

func writeLink(
	title string, linkDataChan *LinkDataChan, anotherDataChan *LinkDataChan) {
	for {
		writer := <-linkDataChan.writerChan
		log.Printf("%s: write -- start", title)
		for {
			buf := <-linkDataChan.toDataChan
			log.Printf("%s: write -- buf, %d", title, buf.len)
			for writer != linkDataChan.currentWriter {
				log.Printf("%s: change writer, %v", title, linkDataChan.currentWriter)
				writer = <-linkDataChan.writerChan
			}
			_, err := writer.Write(buf.buf[0:buf.len])
			anotherDataChan.fromDataChan <- buf
			if err != nil {
				break
			}
		}
		log.Printf("%s: write -- end", title)
	}
}

func readLink(
	title string, linkDataChan *LinkDataChan, anotherDataChan *LinkDataChan) {
	go writeLink(title, linkDataChan, anotherDataChan)
	for {
		stream := <-linkDataChan.connChan
		log.Printf("%s: read -- start", title)
		linkDataChan.currentWriter = stream
		linkDataChan.writerChan <- stream
		for {
			buf := <-linkDataChan.fromDataChan
			size, err := stream.Read(buf.buf)
			if err != nil {
				linkDataChan.currentWriter = nil
				linkDataChan.fromDataChan <- buf
				break
			} else {
				buf.len = size
				anotherDataChan.toDataChan <- buf
			}
		}
		log.Printf("%s: read -- end", title)
		linkDataChan.endChan <- stream
	}
}

func LinkProc(linkParam *LinkParam) {
	go readLink("ws", &linkParam.wsLinkDataChan, &linkParam.tcpLinkDataChan)
	go readLink("tcp", &linkParam.tcpLinkDataChan, &linkParam.wsLinkDataChan)
}
