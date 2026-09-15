package config

import "flag"
import "fmt"
import "os"
import "strconv"

type TFTPConfig struct {
	enabled   bool
	bindAddr  string
	bindPort  int
	blockSize int  
}

type DHCPConfig struct {
    enabled       bool
    mode          string
    bindAddr      string
    bindInterface string
	bindPort	  int
    tftpIP        string
    tftpPort      int
    nextServerIP  string // Add server IP for consistent responses
    bootFilePath  string // Add boot file path configuration
}

type Master struct {
	TFTP *TFTPConfig
	DHCP *DHCPConfig
	General *GeneralConfig
}

type GeneralConfig struct {
	secretKey string
}

func Gather() *Master {
	var (
		kubeconfig = flag.String("kubeconfig", "/tmp/admin.conf", "Path of admin.conf")
		insecure = flag.Bool("insecure", false, "skip TLS Verification")
	)

	flag.Parse()

	fmt.Println(*kubeconfig)
	fmt.Println(*insecure)

	// Initialize Struct with Values first
	tftpConfig := TFTPConfig{
		enabled:   true,
		bindAddr:  "0.0.0.0",
		bindPort:  69,
		blockSize: 512  ,
	}
	
	//TFTP  Forsaken declaration
	if val := os.Getenv("TFTP_ENABLE"); val != "" {
		if boolVal,err := strconv.ParseBool(val); err == nil {
			tftpConfig.enabled = boolVal
		}
	}

	if val :=  os.Getenv("TFTP_BIND_ADDR"); val != "" {
		tftpConfig.bindAddr = val
	}

	if val :=  os.Getenv("TFTP_BIND_PORT"); val != "" {
		if intVal,err := strconv.Atoi(val); err == nil {
			tftpConfig.bindPort = intVal
		}
	}

	if val :=  os.Getenv("TFTP_BLOCKSIZE"); val != "" {
		if intVal,err := strconv.Atoi(val); err == nil {
			tftpConfig.blockSize = intVal
		}
	}

	//
	// DHCP Handler Structure
	// type DHCPConfig struct {
	//     enabled       bool
	//     mode          string
	//     bindAddr      string
	//     bindInterface string
	//     tftpIP        string
	//     tftpPort      int
	//     serverIP      string // Add server IP for consistent responses
	//     bootFilePath  string // Add boot file path configuration
	// }
	//
	// All of these dummy values 
	// should be overrided from 
	// Env variable
	//

	dhcpConfig := DHCPConfig{
		enabled: true,
		mode: "", // Undefiend will be removed 
		bindAddr: "0.0.0.0",
		bindPort: 67,
		bindInterface: "eth0",
		tftpIP: "192.168.1.1", // Dummy endpoint for TFTP
		tftpPort: 69,
		nextServerIP: "192.168.1.1", // Dummy endpoint for GW
		bootFilePath: "pxelinux.0", // Default path
	}

	generalConfig := GeneralConfig{}

	if val := os.Getenv("SECRET_KEY"); val != "" {
		generalConfig.secretKey = val
	}

	if val := os.Getenv("DHCP_ENABLE"); val != "" {
		if boolVal,err := strconv.ParseBool(val); err == nil {
			dhcpConfig.enabled = boolVal
		}
	}

	if val := os.Getenv("DHCP_MODE"); val != "" {
		dhcpConfig.mode = val
	}

	if val := os.Getenv("DHCP_BIND_INTERFACE"); val != "" {
		dhcpConfig.bindInterface = val
	}

	if val := os.Getenv("DHCP_BIND_ADDRESS"); val != "" {
		dhcpConfig.bindAddr = val
	}

	if val := os.Getenv("DHCP_BIND_PORT"); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			dhcpConfig.bindPort = intVal
		}
	}

	if val := os.Getenv("DHCP_TFTP_IP"); val != "" {
		dhcpConfig.tftpIP = val
	}
	if val := os.Getenv("DHCP_TFTP_PORT"); val != "" {
		if intVal,err := strconv.Atoi(val); err == nil {
			dhcpConfig.tftpPort = intVal
		}
	}
	if val := os.Getenv("DHCP_NEXT_SERVER_IP"); val != "" {
		dhcpConfig.nextServerIP = val
	}
	if val := os.Getenv("DHCP_BOOT_FILE"); val != "" {
		dhcpConfig.bootFilePath = val
	}

	return &Master {
		TFTP: &tftpConfig,
		DHCP: &dhcpConfig,
		General: &generalConfig,
	}
}

// DHCP Methods
func (d *DHCPConfig) BindAddr() string { return d.bindAddr }
func (d *DHCPConfig) Enabled() bool { return d.enabled }
func (d *DHCPConfig) Mode() string { return d.mode }         
func (d *DHCPConfig) BindInterface() string { return d.bindInterface }
func (d *DHCPConfig) BindPort() int { return d.bindPort }
func (d *DHCPConfig) TFTPIP() string { return d.tftpIP }
func (d *DHCPConfig) TFTPPort() int { return d.tftpPort }
func (d *DHCPConfig) NextServe() string { return d.nextServerIP }
func (d *DHCPConfig) BootFilePath() string { return d.bootFilePath }

// TFTP Methods
func (t *TFTPConfig) BindAddr() string { return t.bindAddr }
func (t *TFTPConfig) Enabled() bool { return t.enabled }
func (t *TFTPConfig) BindPort() int { return t.bindPort }
func (t *TFTPConfig) BlockSize() int { return t.blockSize }

// General Methods
func (g *GeneralConfig) GetSecret() string { return g.secretKey } 

