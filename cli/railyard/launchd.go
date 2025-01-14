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
			if err := cli.setup(); err != nil {
				return err
			}

			return cli.enableDaemon()
		},
	}

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

func (cli *Cli) enableDaemon() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	user, err := user.Current()
	if err != nil {
		return fmt.Errorf("failed to get current user: %w", err)
	}

	plist, id, err := cli.generatePlist(cli.Suffix)
	if err != nil {
		return fmt.Errorf("failed to generate plist: %w", err)
	}

	plistPath := filepath.Join(home, "Library/LaunchAgents", id+".plist")

	target := "gui/" + user.Uid + "/" + id

	if bs, _ := os.ReadFile(plistPath); !bytes.Equal(bs, plist) {
		_ = bootout(target)

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

	if err = cli.upsertContext(); err != nil {
		log.Warnf("failed to update context: %v", err)
	} else if err = cli.selectContext(); err != nil {
		log.Warnf("failed to select context: %v", err)
	}

	return nil
}

func bootout(serviceTarget string) error {
	if exec.Command("launchctl", "print", serviceTarget) != nil {
		// service is not loaded
		return nil
	}

	if out, err := exec.Command("launchctl", "bootout", serviceTarget).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to unload %s: %s: %w", serviceTarget, string(out), err)
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

func (cli *Cli) generatePlist(suffix string) ([]byte, string, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get executable path: %w", err)
	}

	id := config.AppID + suffix

	doc := plist.PropertyList{
		"Label":   plist.String(id),
		"Program": plist.String(self),
		"ProgramArguments": plist.Array{
			plist.String(fmt.Sprintf("%sd", config.Name)),
			plist.String(fmt.Sprintf("%d", len(cli.Config.DockerSocket.HostPath))),
		},
	}

	if cli.SearchPath != defaultSearchPath() {
		doc["EnvironmentVariables"] = plist.Dict{config.HomeEnv: plist.String(cli.SearchPath)}
	}

	if suffix != "" {
		doc["StandardOutPath"] = plist.String(filepath.Join(cli.Home, "railyard-daemon.out"))
		doc["StandardErrorPath"] = plist.String(filepath.Join(cli.Home, "railyard-daemon.err"))
	}

	sockets := make(plist.Dict, len(cli.Config.DockerSocket.HostPath)+1)
	doc["Sockets"] = sockets

	nw, addr, err := cli.Config.ControlSocket.ResolveListenSocket(cli.Env, cli.Home)
	if err != nil {
		return nil, "", fmt.Errorf("failed to resolve control socket: %w", err)
	}

	sockets["control"] = socket(nw, addr)

	for i, sock := range cli.Config.DockerSocket.HostPath {
		nw, addr, err := sock.ResolveListenSocket(cli.Env, cli.Home)
		if err != nil {
			return nil, "", fmt.Errorf("failed to resolve socket %s: %w", sock.Original, err)
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
			//nolint:wrapcheck
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

func defaultSearchPath() string {
	return os.ExpandEnv(config.UserHomeDir) + ":" + os.ExpandEnv(config.SysHomeDir)
}
