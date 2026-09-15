package main

import (
	"fmt"
	"encoding/csv"
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"golang.org/x/crypto/ssh"

	"github.com/fsnotify/fsnotify"
	"github.com/motangpuar/o2-ims-worker/internal/config"
	"github.com/motangpuar/o2-ims-worker/internal/db"
	"github.com/motangpuar/o2-ims-worker/internal/dhcp"
	"github.com/motangpuar/o2-ims-worker/internal/http"
	"github.com/motangpuar/o2-ims-worker/internal/tftp"
)

//import "github.com/motangpuar/o2-ims-worker/internal/ansible"

//"time"

func main()  {

	// --------<*>----------
	disableTFTP := flag.Bool("no-tftp",false,"Disable TFTP")
	disableDHCP := flag.Bool("no-dhcp",false,"Disable DHCP")
	disableHTTP := flag.Bool("no-http",false,"Disable HTTP")
	flag.Parse()

	// tConfig, dConfig := config.Gather()
	cfg := config.Gather()

	// FileData Watcher
	log.Println("[*]------------------------------------------------------")
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Missing inputs/clients.csv ; Please create and populate")
		log.Fatal(err)
	}

	defer watcher.Close()

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Name == "inputs/clients.csv" {
					log.Println("[*] Filename Filter:", event.Name) 
					filedata.Populate(cfg.General.GetSecret())
					continue
				}
				log.Printf("File Watcher Event: %s ", event.String()) 
				log.Printf("Doing nothing on: %s ", event.Name) 
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("File Watcher Error:", err)
			}
		}
	}()

	// Make sure essential path exists
	essentialPaths := []string{
		"./inputs/",
		"./assets/keys/",
		"./templates/keys/",
		"assets/http/",
		"assets/tftp/bios/pxelinux.cfg/",
		"assets/tftp/efi/grub/x86_64-efi",
	}

	for _,path := range essentialPaths {
		err := makeEssentialPaths(path)
		if err != nil {
			log.Fatalf("[MAIN] Error creating path %s with error:\n %v", path, err.Error())
		}
	}

	// Generate SSH Key
	sshKeyPath := cfg.General.GetSSHKeyPath()
	if err := GenerateSSHKeyPair(sshKeyPath); err != nil {
		log.Printf("[MAIN] SSH Error %v", err)
		os.Exit(1)
	}
	log.Printf("[SUCCESS] SSH key pair generated at ./assets/keys")


	// Check CSV
	err = EnsureMachinesCSV("inputs/clients.csv")

	if err != nil {
		log.Fatalf("[MAIN] Failed to create client.csv: %v: err", err)
	}


	// Init filedata 
	filedata.Populate(cfg.General.GetSecret())

	// Create Pointer for TFTP & DHCP Config
	tftpCfgPtr := cfg.TFTP
	dhcpCfgPtr := cfg.DHCP

	//----------------------------------------------
	log.Println("[DHCP].........")
	log.Println(dhcpCfgPtr.BindAddr())
	log.Println(dhcpCfgPtr.Enabled())
	log.Println(dhcpCfgPtr.Mode())
	log.Println(dhcpCfgPtr.BindInterface())
	log.Println(dhcpCfgPtr.TFTPIP())
	log.Println(dhcpCfgPtr.TFTPPort())
	log.Println(dhcpCfgPtr.NextServe())
	log.Println(dhcpCfgPtr.BootFilePath())
	log.Println()

	//----------------------------------------------
	log.Println("[TFTP].........")
	log.Println(tftpCfgPtr.BindAddr())
	log.Println(tftpCfgPtr.Enabled())
	log.Println(tftpCfgPtr.BindAddr())
	log.Println(tftpCfgPtr.BindPort())
	log.Println(tftpCfgPtr.BlockSize())
	log.Println()


	ctx,cancel := context.WithCancel(context.Background())
	defer cancel()

	if *disableTFTP != true {
		e := tftp.NewEngine(tftpCfgPtr)
		go e.Start()
	}

	if *disableDHCP != true {
		d := dhcp.NewEngine(dhcpCfgPtr)
		go d.Start()
	}

	if *disableHTTP != true {
		go http_handler.Serve(ctx)
	}

	// Wait for it to stop
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	cancel()

}


func makeEssentialPaths(path string) error {
	// Make sure assers path exists
	_,err := os.Stat(path)
	if os.IsNotExist(err) {
		err := os.MkdirAll(path, os.ModePerm)
		if err != nil {
			log.Fatal(err)
		}
		return nil
	} else if err != nil {
			log.Fatal(err)
	} else {
		log.Printf("[MAIN] %s path exists!",path)
		return nil
	}
	return err
}

func EnsureMachinesCSV(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	w.Write([]string{"IP", "MAC", "BOOTFILE", "TYPE", "CLUSTER", "TEMPLATE", "ROLE", "GATEWAY"})
	w.Flush()
	return w.Error()
}


func GenerateSSHKeyPair(keyPath string) error {

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate key pair: %w", err)
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privBytes,
	})

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return fmt.Errorf("failed to convert public key: %w", err)
	}
	pubAuthKey := ssh.MarshalAuthorizedKey(sshPub)

	privPath := keyPath
	pubPath := keyPath + ".pub"

	if err := os.WriteFile(privPath, privPEM, 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}
	if err := os.WriteFile(pubPath, pubAuthKey, 0644); err != nil {
		return fmt.Errorf("failed to write public key: %w", err)
	}

	return nil
}

