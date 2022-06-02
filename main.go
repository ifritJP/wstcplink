// -*- coding: utf-8 -*-
package main

import (
	"bufio"
	"flag"
	"fmt"

	//"io"
	"net/url"
	"os"

	//"os/signal"
	"strconv"
	"strings"
	//	"time"
)

const VERSION = "0.0.1"

func hostname2HostInfo(name string) *HostInfo {
	if strings.Index(name, "://") == -1 {
		name = fmt.Sprintf("http://%s", name)
	}
	serverUrl, err := url.Parse(name)
	if err != nil {
		fmt.Printf("%s\n", err)
		return nil
	}
	hostport := strings.Split(serverUrl.Host, ":")
	if len(hostport) != 2 {
		fmt.Printf("illegal pattern. set 'hoge.com:1234' -- %s\n", name)
		return nil
	}
	var port int
	port, err2 := strconv.Atoi(hostport[1])
	if err2 != nil {
		fmt.Printf("%s\n", err2)
		return nil
	}
	return &HostInfo{"", hostport[0], port, serverUrl.Path}
}

var verboseFlag = false

func IsVerbose() bool {
	return verboseFlag
}

func main() {

	var cmd = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	version := cmd.Bool("version", false, "display the version")
	help := cmd.Bool("help", false, "display help message")
	cmd.Usage = func() {
		fmt.Fprintf(cmd.Output(), "\nUsage: %s <mode [-help]> [-version]\n\n", os.Args[0])
		fmt.Fprintf(cmd.Output(), " mode: \n")
		fmt.Fprintf(cmd.Output(), "    server\n")
		fmt.Fprintf(cmd.Output(), "    test-wsclient\n")
		fmt.Fprintf(cmd.Output(), "    help\n")
		os.Exit(1)
	}
	cmd.Parse(os.Args[1:])

	if *version {
		fmt.Printf("version: %s\n", VERSION)
		os.Exit(0)
	}
	if *help {
		cmd.Usage()
		os.Exit(0)
	}
	if len(cmd.Args()) > 0 {
		switch mode := cmd.Args()[0]; mode {
		case "server":
			ParseOptServer(mode, cmd.Args()[1:])
		case "test-wsclient":
			ParseOptTestWSClient(cmd.Args())
		default:
			cmd.Usage()
			os.Exit(1)
		}
		os.Exit(0)
	}
	cmd.Usage()
	os.Exit(1)
}

func ParsePort(arg string) *LinkPort {
	tokenList := strings.Split(arg, ",")
	if len(tokenList) != 2 {
		return nil
	}
	wsServerInfo := hostname2HostInfo(tokenList[0])
	if wsServerInfo == nil {
		return nil
	}
	tcpServerInfo := hostname2HostInfo(tokenList[1])
	if tcpServerInfo == nil {
		return nil
	}
	return &LinkPort{
		wsServerInfo:  *wsServerInfo,
		tcpServerInfo: *tcpServerInfo,
	}
}

func ParseOpt(
	cmd *flag.FlagSet, mode string, args []string) (*TunnelParam, []*LinkPort) {

	ipPattern := cmd.String("ip", "", "allow ip range (192.168.0.1/24)")
	verbose := cmd.Bool("verbose", false, "verbose. (true or false)")

	usage := func() {
		fmt.Fprintf(cmd.Output(), "\nUsage: %s %s pair [pair ...] ", os.Args[0], mode)
		fmt.Fprintf(cmd.Output(), "[option] \n\n")
		fmt.Fprintf(cmd.Output(), "   pair: wsserver and tcpserver address.\n")
		fmt.Fprintf(cmd.Output(), "         e.g. localhost:10000,localhost:10001 or :10000,:10001\n")

		fmt.Fprintf(cmd.Output(), "\n")
		fmt.Fprintf(cmd.Output(), " options:\n")
		cmd.PrintDefaults()
		os.Exit(1)
	}
	cmd.Usage = usage

	cmd.Parse(args)

	nonFlagArgs := []string{}
	for len(cmd.Args()) != 0 {
		workArgs := cmd.Args()

		findOp := false
		for index, arg := range workArgs {
			if strings.Index(arg, "-") == 0 {
				cmd.Parse(workArgs[index:])
				findOp = true
				break
			} else {
				nonFlagArgs = append(nonFlagArgs, arg)
			}
		}
		if !findOp {
			break
		}
	}
	if len(nonFlagArgs) < 1 {
		usage()
	}

	linkPortList := []*LinkPort{}

	for _, nonFlagArg := range nonFlagArgs {
		linkPort := ParsePort(nonFlagArg)
		if linkPort == nil {
			fmt.Print("set wsserver option!\n")
			usage()
		}
		linkPortList = append(linkPortList, linkPort)
	}

	var maskIP *MaskIP = nil
	if *ipPattern != "" {
		var err error
		maskIP, err = ippattern2MaskIP(*ipPattern)
		if err != nil {
			fmt.Println(err)
			usage()
		}
	}

	verboseFlag = *verbose

	param := TunnelParam{
		maskedIP:   maskIP,
		maxSession: len(linkPortList) * 2,
	}

	return &param, linkPortList
}

func ParseOptServer(mode string, args []string) {
	var cmd = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	param, linkPortList := ParseOpt(cmd, mode, args)

	for _, linkPort := range linkPortList {
		linkParam := CreateLinkParam()
		go LinkProc(linkParam)
		go StartServer(param, linkPort.tcpServerInfo, &linkParam.tcpLinkDataChan)
		go StartWebsocketServer(
			param, linkPort.wsServerInfo, &linkParam.wsLinkDataChan)
	}

	endChan := make(chan bool, 1)
	<-endChan
}

func ParseOptTestWSClient(args []string) {
	var cmd = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	usage := func() {
		fmt.Fprintf(cmd.Output(), "\nUsage: %s <wsserver> ", args[0])
		fmt.Fprintf(cmd.Output(), "[option] \n\n")
		fmt.Fprintf(cmd.Output(), "   wsserver: e.g. localhost:1234 or :1234\n")
		fmt.Fprintf(cmd.Output(), "\n")
		fmt.Fprintf(cmd.Output(), " options:\n")
		cmd.PrintDefaults()
		os.Exit(1)
	}
	cmd.Usage = usage

	if len(args) != 2 {
		usage()
	}

	wsServerInfo := hostname2HostInfo(args[1])
	if wsServerInfo == nil {
		fmt.Print("set wsserver option!\n")
		usage()
	}

	ConnectWebScoket(fmt.Sprintf("ws://%s:%d/", wsServerInfo.Name, wsServerInfo.Port))
}

func test() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		fmt.Println(scanner.Text()) // Println will add back the final '\n'
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "reading standard input:", err)
	}
}
