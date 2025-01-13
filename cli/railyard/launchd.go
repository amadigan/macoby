package railyard

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"

	"github.com/amadigan/macoby/internal/config"
	"github.com/amadigan/macoby/internal/plist"
	"github.com/spf13/cobra"
)

func NewEnableCommand(cli *Cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable railyard daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			return enableDaemon(cli)
		},
	}

	return cmd
}

func NewDisableCommand(cli *Cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Disable railyard daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			return disableDaemon(cli)
		},
	}

	return cmd
}

func enableDaemon(cli *Cli) error {
	if err := cli.setup(); err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	user, err := user.Current()
	if err != nil {
		return fmt.Errorf("failed to get current user: %w", err)
	}

	plist, id, err := cli.generatePlist()
	if err != nil {
		return fmt.Errorf("failed to generate plist: %w", err)
	}

	plistPath := filepath.Join(home, "Library/LaunchAgents", id+".plist")

	if bs, _ := os.ReadFile(plistPath); !bytes.Equal(bs, plist) {
		_ = remove(user.Uid, plistPath)

		//nolint:gosec
		if err := os.WriteFile(plistPath, plist, 0644); err != nil {
			return fmt.Errorf("failed to write plist: %w", err)
		}

		//nolint:gosec
		cmd := exec.Command("launchctl", "bootstrap", "gui/"+user.Uid, plistPath)
		out, err := cmd.CombinedOutput()

		if err != nil {
			return fmt.Errorf("failed to load %s: %s: %w", plistPath, string(out), err)
		}
	}

	return nil
}

func remove(uid, path string) error {
	if err := os.Remove(path); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", path, err)
		}
	}

	//nolint:gosec
	cmd := exec.Command("launchctl", "bootout", "gui/"+uid, path)
	out, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("failed to unload %s: %s: %w", path, string(out), err)
	}

	return nil
}

func disableDaemon(cli *Cli) error {
	if err := cli.setup(); err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	suffix := getSuffix(cli.Home)
	id := config.AppID + suffix

	plistPath := filepath.Join(home, "Library/LaunchAgents", id+".plist")

	user, err := user.Current()
	if err != nil {
		return fmt.Errorf("failed to get current user: %w", err)
	}

	return remove(user.Uid, plistPath)
}

func (cli *Cli) generatePlist() ([]byte, string, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get executable path: %w", err)
	}

	suffix := getSuffix(cli.Home)
	id := config.AppID + suffix

	doc := plist.PropertyList{
		"Label":   plist.String(id),
		"Program": plist.String(self),
		"ProgramArguments": plist.Array{
			plist.String(fmt.Sprintf("%sd", config.Name)),
			plist.String(fmt.Sprintf("%d", len(cli.Config.DockerSocket.HostPath))),
		},
		"EnvironmentVariables": plist.Dict{
			config.HomeEnv: plist.String(cli.SearchPath),
		},
	}

	if suffix != "" {
		doc["StandardOutPath"] = plist.String(filepath.Join(cli.Home, "railyard-daemon.out"))
		doc["StandardErrorPath"] = plist.String(filepath.Join(cli.Home, "railyard-daemon.err"))
	}

	sockets := make(plist.Dict, len(cli.Config.DockerSocket.HostPath)+1)
	doc["Sockets"] = sockets

	nw, addr, err := cli.Config.ControlSocket.ResolveListenSocket(cli.Env, cli.Home)
	if err != nil {
		return nil, "", err
	}

	sockets["control"] = socket(nw, addr)

	for i, sock := range cli.Config.DockerSocket.HostPath {
		nw, addr, err := sock.ResolveListenSocket(cli.Env, cli.Home)
		if err != nil {
			return nil, "", err
		}

		sockets[fmt.Sprintf("docker%d", i)] = socket(nw, addr)
	}

	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")

	err = encodeTokens(enc,
		xml.ProcInst{Target: "xml", Inst: []byte(`version="1.0" encoding="UTF-8"`)}, xml.CharData("\n"),
		xml.Directive(`DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"`),
		xml.CharData("\n"))

	if err != nil {
		return nil, "", fmt.Errorf("failed to encode plist: %w", err)
	}

	if err := enc.Encode(doc); err != nil {
		return nil, "", fmt.Errorf("failed to encode plist: %w", err)
	}

	if err := enc.EncodeToken(xml.CharData("\n")); err != nil {
		return nil, "", fmt.Errorf("failed to encode plist: %w", err)
	}

	if err := enc.Flush(); err != nil {
		return nil, "", fmt.Errorf("failed to encode plist: %w", err)
	}

	return buf.Bytes(), id, nil
}

func encodeTokens(enc *xml.Encoder, tokens ...xml.Token) error {
	for _, tok := range tokens {
		if err := enc.EncodeToken(tok); err != nil {
			return err
		}
	}

	return nil
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

func getSuffix(home string) string {
	defaultHome := os.ExpandEnv(config.UserHomeDir)

	if home == defaultHome {
		return ""
	}

	return "-" + filepath.Base(home)
}

func defaultSearchPath() string {
	return os.ExpandEnv(config.UserHomeDir) + ":" + os.ExpandEnv(config.SysHomeDir)
}
