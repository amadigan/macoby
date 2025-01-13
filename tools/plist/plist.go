package main

import (
	"encoding/xml"
	"fmt"
	"os"

	"github.com/amadigan/macoby/internal/config"
	"github.com/amadigan/macoby/internal/plist"
	"github.com/amadigan/macoby/internal/util"
)

func main() {
	self, err := os.Executable()
	if err != nil {
		panic(err)
	}

	dir := ""

	if len(os.Args) > 1 {
		dir = os.Args[1]
	}

	env := util.Env()

	root, home := config.BuildHomePath(env, dir)

	env[config.HomeEnv] = home

	confPath := &config.Path{Original: fmt.Sprintf("${%s}/%s.jsonc", config.HomeEnv, config.Name)}

	if !confPath.ResolveInputFile(env, root) {
		panic(fmt.Errorf("failed to resolve input file: %s", confPath.Original))
	}

	var layout config.Layout
	if err := util.ReadJsonConfig(confPath.Resolved, &layout); err != nil {
		panic(fmt.Errorf("failed to read railyard.json: %w", err))
	}

	layout.SetDefaultSockets()

	doc := plist.PropertyList{
		"Label":   plist.String(config.AppID),
		"Program": plist.String(self),
		"EvironmentVariables": plist.Dict{
			config.HomeEnv: plist.String(home),
		},
	}

	sockets := plist.Array{}

	nw, addr, err := layout.ControlSocket.ResolveListenSocket(env, root)
	if err != nil {
		panic(err)
	}

	sockets = append(sockets, socket(nw, addr))

	for _, sock := range layout.DockerSocket.HostPath {
		nw, addr, err := sock.ResolveListenSocket(env, root)
		if err != nil {
			panic(err)
		}

		sockets = append(sockets, socket(nw, addr))
	}

	doc["Sockets"] = sockets

	enc := xml.NewEncoder(os.Stdout)

	enc.Indent("", "  ")

	enc.EncodeToken(xml.ProcInst{Target: "xml", Inst: []byte(`version="1.0" encoding="UTF-8"`)})
	enc.EncodeToken(xml.CharData("\n"))
	enc.EncodeToken(xml.Directive(`DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"`))
	enc.EncodeToken(xml.CharData("\n"))

	if err := enc.Encode(doc); err != nil {
		panic(err)
	}

	enc.EncodeToken(xml.CharData("\n"))
	enc.Flush()
}

func socket(network, addr string) plist.Dict {
	if network == "unix" {
		return plist.Dict{
			"SockPathName": plist.String(addr),
		}
	}

	dict := plist.Dict{
		"SockProtocol":    plist.String("TCP"),
		"SockServiceName": plist.String(addr),
	}

	switch network {
	case "tcp":
		dict["SockFamily"] = plist.String("IPv4v6")
	case "tcp4":
		dict["SockFamily"] = plist.String("IPv4")
	case "tcp6":
		dict["SockFamily"] = plist.String("IPv6")
	}

	return dict
}
