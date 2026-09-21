package main

// fvcs-cli —— FVCS 维护命令行（07 §5.5 M3：`fvcs-cli reencrypt` 一次性迁移工具）。
//
// 用法：
//	fvcs-cli reencrypt [-dry-run] [-config <path>]   旧硬编码密钥密文 → DPAPI 主密钥重写
//	fvcs-cli status    [-config <path>]              查看配置文件 / 主密钥 / 加密状态
//	fvcs-cli version                                 打印版本
//	fvcs-cli help                                    帮助
//
// 部署约定：本程序须与 FVCS 服务（fvcs.exe）位于同一目录，才能读到同一份
// master.key 与 config.enc.json；迁移前建议先停止服务进程。

import (
	"flag"
	"fmt"
	"os"

	"Fnos.VC_Service/pkg/config"
	"Fnos.VC_Service/pkg/version"
)

const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(exitUsage)
	}

	switch os.Args[1] {
	case "reencrypt":
		os.Exit(cmdReencrypt(os.Args[2:]))
	case "status":
		os.Exit(cmdStatus(os.Args[2:]))
	case "version", "-v", "--version":
		fmt.Printf("fvcs-cli %s (M3)\n", version.Version)
		os.Exit(exitOK)
	case "help", "-h", "--help":
		usage()
		os.Exit(exitOK)
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n\n", os.Args[1])
		usage()
		os.Exit(exitUsage)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `fvcs-cli —— FVCS 维护命令行

用法:
  fvcs-cli reencrypt [-dry-run] [-config <path>]   配置密钥迁移（旧硬编码密钥 → DPAPI 主密钥）
  fvcs-cli status    [-config <path>]              查看配置文件/主密钥/加密状态
  fvcs-cli version                                 打印版本
  fvcs-cli help                                    显示本帮助

说明:
  - 本程序需与 fvcs.exe 同目录部署（共用同一 master.key 与 config.enc.json）。
  - 迁移只更换加密密钥，配置内容不变；非旧密文文件不会被改动（可重复执行）。
  - 执行前请先停止 FVCS 服务进程。
`)
}

// cmdReencrypt 执行 / 预检 M3 迁移。
func cmdReencrypt(args []string) int {
	fs := flag.NewFlagSet("reencrypt", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "只检测是否需要迁移，不写盘")
	cfgPath := fs.String("config", "", "配置文件路径（默认 exe 同级 config.enc.json）")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	path := *cfgPath
	if path == "" {
		path = config.ConfigPath()
	}

	fmt.Printf("配置文件 : %s\n", path)
	fmt.Printf("主密钥   : %s\n", config.MasterKeyPath())

	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(os.Stderr, "读取配置失败: %v\n", err)
		return exitFail
	}

	rewritten, err := config.ReencryptFile(path, *dryRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "迁移失败（未改动原文件）: %v\n", err)
		return exitFail
	}
	if !rewritten {
		fmt.Println("当前密钥 : DPAPI 主密钥（无需迁移）")
		return exitOK
	}
	if *dryRun {
		fmt.Println("当前密钥 : 旧硬编码密钥")
		fmt.Println("检测结果 : 需要迁移（未写盘；去掉 -dry-run 执行）")
		return exitOK
	}
	fmt.Println("迁移结果 : 已用 DPAPI 主密钥重写（配置内容未变）")
	return exitOK
}

// cmdStatus 打印配置与主密钥状态（只读，不生成密钥、不改动配置）。
func cmdStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "配置文件路径（默认 exe 同级 config.enc.json）")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	path := *cfgPath
	if path == "" {
		path = config.ConfigPath()
	}

	fmt.Printf("配置文件 : %s", path)
	if _, err := os.Stat(path); err != nil {
		fmt.Printf("（不存在）\n")
	} else {
		fmt.Printf("（存在）\n")
	}

	keyPath := config.MasterKeyPath()
	if _, err := os.Stat(keyPath); err != nil {
		fmt.Printf("主密钥   : %s（未生成，下次启动服务或迁移时创建）\n", keyPath)
	} else {
		fmt.Printf("主密钥   : %s（存在，DPAPI 保护）\n", keyPath)
	}

	if _, err := os.Stat(path); err != nil {
		return exitOK
	}
	need, err := config.NeedsReencrypt(path)
	switch {
	case err != nil:
		fmt.Printf("加密状态 : 无法判定（%v）\n", err)
		return exitFail
	case need:
		fmt.Println("加密状态 : 旧硬编码密钥，建议执行 fvcs-cli reencrypt")
	default:
		fmt.Println("加密状态 : DPAPI 主密钥")
	}
	return exitOK
}
