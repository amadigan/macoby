package railyard

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/amadigan/macoby/internal/host/config"
	"github.com/amadigan/macoby/internal/util"
	"github.com/spf13/cobra"
)

type daemonOptions struct {
	Debug bool
}

func NewEnableCommand(cli *Cli) *cobra.Command {
	var do daemonOptions

	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable railyard daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cli.setup(); err != nil {
				return err
			}

			return cli.enableDaemon(do)
		},
	}

	cmd.Flags().BoolVarP(&do.Debug, "debug", "d", false, "Enable debug logging")

	return cmd
}

func NewDisableCommand(cli *Cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Disable railyard daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cli.setup(); err != nil {
				return err
			}

			return cli.disableDaemon()
		},
	}

	return cmd
}

func (cli *Cli) enableDaemon(do daemonOptions) error {
	if cli.ConfigPath.Resolved == "" {
		if err := config.WriteDefaultConfig(cli.Env, cli.Config.Home, cli.ConfigPath); err != nil {
			log.Warnf("failed to write default config: %v", err)
		}
	}

	ctl, err := cli.newLaunchdControl()
	if err != nil {
		return err
	}

	plistBs, _, err := cli.generatePlist(do.Debug)
	if err != nil {
		return err
	}

	if err := ctl.Update(context.Background(), plistBs); err != nil {
		return err
	}

	if err = cli.upsertContext(); err != nil {
		log.Warnf("failed to update context: %v", err)
	} else if err = cli.selectContext(); err != nil {
		log.Warnf("failed to select context: %v", err)
	}

	return nil
}

func bootout(serviceTarget string) error {
	log.Infof("unloading %s", serviceTarget)

	for exec.Command("launchctl", "print", serviceTarget) != nil {
		if out, err := exec.Command("launchctl", "bootout", serviceTarget, "socket").CombinedOutput(); err != nil {
			var exitErr *exec.ExitError

			if errors.As(err, &exitErr) && exitErr.ExitCode() == 3 {
				log.Infof("service %s unloaded", serviceTarget)

				return nil
			}

			return fmt.Errorf("failed to unload %s: %s: %w", serviceTarget, string(out), err)
		}

		time.Sleep(time.Second)
	}

	return nil
}

func (cli *Cli) disableDaemon() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	id := config.AppID + cli.Suffix
	plistPath := filepath.Join(home, "Library/LaunchAgents", id+".plist")

	user, err := user.Current()
	if err != nil {
		return fmt.Errorf("failed to get current user: %w", err)
	}

	target := "gui/" + user.Uid + "/" + id

	if err := bootout(target); err != nil {
		log.Warnf("failed to unload %s: %v", target, err)
	} else {
		log.Infof("unloaded %s", target)
	}

	if os.Remove(plistPath) == nil {
		log.Infof("removed %s", plistPath)
	}

	return nil
}

func (cli *Cli) generatePlist(debug bool) ([]byte, string, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get executable path: %w", err)
	}

	id := config.AppID + cli.Suffix

	doc := PropertyList{
		"Label":   String(id),
		"Program": String(self),
		"ProgramArguments": Array{
			String(fmt.Sprintf("%sd", config.Name)),
			String(fmt.Sprintf("%d", len(cli.Config.DockerSocket.HostPath))),
		},
		"ExitTimeOut": Integer(35), //TODO: this should be based on the daemon's shutdown timeout
		"ProcessType": String("Interactive"),
	}

	if cli.SearchPath != defaultSearchPath() {
		doc["EnvironmentVariables"] = Dict{config.HomeEnv: String(cli.SearchPath)}
	}

	if debug {
		doc["StandardOutPath"] = String(filepath.Join(cli.Config.Home, "railyard-daemon.out"))
		doc["StandardErrorPath"] = String(filepath.Join(cli.Config.Home, "railyard-daemon.err"))
	}

	sockets := make(Dict, len(cli.Config.DockerSocket.HostPath))
	doc["Sockets"] = sockets

	for i, sock := range cli.Config.DockerSocket.HostPath {
		nw, addr, err := sock.ResolveListenSocket(cli.Env, cli.Config.Home)
		if err != nil {
			return nil, "", fmt.Errorf("failed to resolve socket %s: %w", sock.Original, err)
		}

		sockets[fmt.Sprintf("docker%d", i)] = socket(nw, addr)
	}

	bs, err := toXML(doc)

	return bs, id, err
}

func toXML(doc PropertyList) ([]byte, error) {
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")

	err := encodeTokens(enc,
		xml.ProcInst{Target: "xml", Inst: []byte(`version="1.0" encoding="UTF-8"`)}, xml.CharData("\n"),
		xml.Directive(`DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"`),
		xml.CharData("\n"))

	if err != nil {
		return nil, fmt.Errorf("failed to encode plist: %w", err)
	}

	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("failed to encode plist: %w", err)
	}

	if err := enc.EncodeToken(xml.CharData("\n")); err != nil {
		return nil, fmt.Errorf("failed to encode plist: %w", err)
	}

	if err := enc.Flush(); err != nil {
		return nil, fmt.Errorf("failed to encode plist: %w", err)
	}

	return buf.Bytes(), nil
}

func encodeTokens(enc *xml.Encoder, tokens ...xml.Token) error {
	for _, tok := range tokens {
		if err := enc.EncodeToken(tok); err != nil {
			//nolint:wrapcheck
			return err
		}
	}

	return nil
}

func socket(network, addr string) Dict {
	if network == "unix" {
		return Dict{
			"SockPathName": String(addr),
			"SockPathMode": Integer(0o600),
		}
	}

	dict := Dict{
		"SockProtocol":    String("TCP"),
		"SockServiceName": String(addr),
	}

	switch network {
	case "tcp":
		dict["SockFamily"] = String("IPv4v6")
	case "tcp4":
		dict["SockFamily"] = String("IPv4")
	case "tcp6":
		dict["SockFamily"] = String("IPv6")
	}

	return dict
}

func defaultSearchPath() string {
	return os.ExpandEnv(config.UserHomeDir) + ":" + os.ExpandEnv(config.SysHomeDir)
}

type launchdControl struct {
	domain string
	label  string
	path   string
}

func (cli *Cli) newLaunchdControl() (*launchdControl, error) {
	id := config.AppID + cli.Suffix

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	user, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("failed to get current user: %w", err)
	}

	return &launchdControl{
		domain: "gui/" + user.Uid,
		label:  id,
		path:   filepath.Join(home, "Library/LaunchAgents", id+".plist"),
	}, nil
}

func run(cmd string, args ...string) error {
	if out, err := exec.Command(cmd, args...).CombinedOutput(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			fullCmd := cmd + " " + strings.Join(args, " ")

			return fmt.Errorf("%s exit status %d: %s (%w)", fullCmd, exitErr.ExitCode(), string(out), err)
		} else {
			return fmt.Errorf("failed to run %s: %w", cmd, err)
		}
	}

	return nil
}

func (lc *launchdControl) Restart() error {
	return run("launchctl", "kickstart", lc.domain+"/"+lc.label)
}

func (lc *launchdControl) Exists() bool {
	return exec.Command("launchctl", "print", lc.domain+"/"+lc.label) == nil //nolint:gosec
}

func (lc *launchdControl) Unload(ctx context.Context) error {
	if err := run("launchctl", "bootout", lc.domain+"/"+lc.label, "socket"); err != nil {
		return err
	}

	for exec.Command("launchctl", "kill", "SIGTERM", lc.domain+"/"+lc.label) == nil { //nolint:gosec
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}

	return nil
}

func (lc *launchdControl) Load() error {
	return run("launchctl", "bootstrap", lc.domain, lc.path)
}

func (lc *launchdControl) Remove(ctx context.Context) error {
	if lc.Exists() {
		if err := lc.Unload(ctx); err != nil {
			return err
		}
	}

	if err := os.Remove(lc.path); err != nil && !os.IsNotExist(err) {
		log.Warnf("failed to remove %s: %w", lc.path, err)
	}

	return nil
}

func (lc *launchdControl) Update(ctx context.Context, data []byte) error {
	if bs, _ := os.ReadFile(lc.path); !bytes.Equal(bs, data) {
		if err := lc.Remove(ctx); err != nil && !os.IsNotExist(err) {
			log.Warnf("failed to remove %s: %v", lc.path, err)
		}

		//nolint:gosec
		if err := os.WriteFile(lc.path, data, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", lc.path, err)
		}

		return lc.Load()
	} else if !lc.Exists() {
		return lc.Load()
	}

	return nil
}

type PropertyList map[string]any
type Dict map[string]any
type String string
type Integer int64
type Boolean bool
type Array []any

func (p PropertyList) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	el := xml.StartElement{Name: xml.Name{Local: "plist"}, Attr: []xml.Attr{{Name: xml.Name{Local: "version"}, Value: "1.0"}}}

	if err := e.EncodeToken(el); err != nil {
		return fmt.Errorf("failed to encode plist start element: %w", err)
	}

	if err := e.Encode(Dict(p)); err != nil {
		return fmt.Errorf("failed to encode plist dict: %w", err)
	}

	if err := e.EncodeToken(el.End()); err != nil {
		return fmt.Errorf("failed to encode plist end element: %w", err)
	}

	return nil
}

func (d Dict) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	el := xml.StartElement{Name: xml.Name{Local: "dict"}}
	if err := e.EncodeToken(el); err != nil {
		return fmt.Errorf("failed to encode dict start element: %w", err)
	}

	for _, key := range util.SortKeys(d) {
		if err := e.EncodeElement(xml.CharData(key), xml.StartElement{Name: xml.Name{Local: "key"}}); err != nil {
			return fmt.Errorf("failed to encode dict key: %w", err)
		}

		if err := e.Encode(d[key]); err != nil {
			return fmt.Errorf("failed to encode dict value: %w", err)
		}
	}

	if err := e.EncodeToken(el.End()); err != nil {
		return fmt.Errorf("failed to encode dict end element: %w", err)
	}

	return nil
}

func (s String) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	el := xml.StartElement{Name: xml.Name{Local: "string"}}
	if err := e.EncodeElement(xml.CharData(s), el); err != nil {
		return fmt.Errorf("failed to encode string: %w", err)
	}

	return nil
}

func (a Array) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	el := xml.StartElement{Name: xml.Name{Local: "array"}}

	if err := e.EncodeToken(el); err != nil {
		return fmt.Errorf("failed to encode array start element: %w", err)
	}

	for _, item := range a {
		if err := e.Encode(item); err != nil {
			return fmt.Errorf("failed to encode array item: %w", err)
		}
	}

	if err := e.EncodeToken(el.End()); err != nil {
		return fmt.Errorf("failed to encode array end element: %w", err)
	}

	return nil
}

func (i Integer) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	el := xml.StartElement{Name: xml.Name{Local: "integer"}}
	if err := e.EncodeElement(xml.CharData(fmt.Sprintf("%d", i)), el); err != nil {
		return fmt.Errorf("failed to encode integer: %w", err)
	}

	return nil
}

func (b Boolean) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	el := xml.StartElement{Name: xml.Name{Local: "true"}}
	if !b {
		el.Name.Local = "false"
	}

	if err := e.EncodeToken(el); err != nil {
		return fmt.Errorf("failed to encode boolean: %w", err)
	}

	return nil
}
