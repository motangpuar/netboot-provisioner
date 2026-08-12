package ansible_worker

import (
	"fmt"
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"encoding/json"
	"github.com/apenella/go-ansible/v2/pkg/playbook"
	"github.com/apenella/go-ansible/v2/pkg/execute"
	ansiblejson "github.com/apenella/go-ansible/v2/pkg/execute/result/json"
	"github.com/apenella/go-ansible/v2/pkg/execute/stdoutcallback"

	//Internals
	"github.com/motangpuar/o2-ims-worker/internal/kubernetes"
)

type template struct {
	machineType string
}

type AnsibleJSONResults struct {
	Plays []struct {
		Tasks []struct {
			Hosts map[string]struct {
				Msg interface{} `json:"msg"`
			} `json:"hosts"`
		} `json:"tasks"`
	} `json:"plays"`
}

type K3sCreds struct {
	Server string `json:"server"`
	Token  string `json:"token"`
}

func writeTemp(pattern string, data []byte, perm os.FileMode)(string, func()){
	f,err := os.CreateTemp("", pattern)
	if err != nil {
		log.Printf("[ANSIBLE] Error Create Temp file for ansible: %v", err)
		return "",nil
	}

	f.Write(data)
	f.Close()
	os.Chmod(f.Name(), perm)

	log.Printf("[ANSIBLE] Temp File name is %s", f.Name())

	//return f.Name(), func() { log.Printf("Not Delete: %s", pattern) }
	return f.Name(), func() { os.Remove(f.Name()) }

}

func extractCreds(res ansiblejson.AnsiblePlaybookJSONResults) (*K3sCreds, error) {
	log.Printf("[EXTRACTION] %T", res)
	for _, play := range res.Plays {
		for _, task := range play.Tasks {
			for _, host := range task.Hosts {
				if host.Msg == nil {
					continue
				}
				s, ok := host.Msg.(string)
				if !ok || s == "" {
					continue
				}
				var creds K3sCreds
				if err := json.Unmarshal([]byte(s), &creds); err != nil {
					continue
				}
				if creds.Server != "" && creds.Token != "" {
					return &creds, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("no valid k3s credentials found")
}

func FetchToken(targetIP, userName, macAddress string) (*K3sCreds, error) {
	sshKey, _ := os.ReadFile("assets/keys/test_provisioner")
	var playbookYAML []byte
	chunk, err := os.ReadFile("templates/ansible/k3s-fetch-token.yaml")
	if err != nil {
		return nil,err
	}

	playbookYAML = chunk
	playbookFile, cleanupPB := writeTemp("playbook-*.yaml", playbookYAML, 0644)
	keyFile, cleanupKF := writeTemp("id_ed25519-*", sshKey, 0600)
	defer cleanupPB()
	defer cleanupKF()
	
	opts := &playbook.AnsiblePlaybookOptions{
		Inventory: targetIP+",",
		User: userName,
		PrivateKey: keyFile,
		SSHExtraArgs:  "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -tt",
		BecomeMethod: "sudo",
	}

	opts.AddExtraVar("ansible_become_pass", "password")
	opts.AddExtraVar("mac_address", macAddress)
	if userName == "ubuntu" {
		opts.AddExtraVar("ansible_become_exe", "sudo.ws")
	}

	cmd := playbook.NewAnsiblePlaybookCmd(
		playbook.WithPlaybooks(playbookFile),
		playbook.WithPlaybookOptions(opts),
	)

	var buff bytes.Buffer
	exec := stdoutcallback.NewJSONStdoutCallbackExecute(
		execute.NewDefaultExecute(
			execute.WithCmd(cmd),
			execute.WithWrite(&buff),
		),
	)

	if err := exec.Execute(context.Background()); err != nil {
		log.Printf("[Ansible-Token] Fail to execute ansible: %v", err)
		return nil,err
	}

	res, err := ansiblejson.ParseJSONResultsStream(&buff)
	dump,err := extractCreds(*res)
	if err != nil {
		log.Printf("[ANSIBLE] Error: %v", err.Error())
		return nil,err
	}

	// Iterate over clusters and add token based on the master
	// to Cluster
	getClusters, err := kubeclient.GetClusters(context.Background())
	for k,c := range getClusters.Cluster {
		if k != "unclaimed" { 
			for _,n := range c.Nodes {
				if n.Node == macAddress {
					c.Token = dump.Token
					c.APIEndpoint = dump.Server
				}
			}
		}
	}

	return dump, nil
}

func Populate(targetIP, macAddress, userName, templateMode string, nodeObj struct {
	Name string
	IP   string
	Mac  string
	Role string
}) (*ansiblejson.AnsiblePlaybookJSONResults,error) {

	sshKey, _ := os.ReadFile("assets/keys/test_provisioner")
	log.Printf("[ANSIBLE] Populate ansible ...")

	cwd, err := os.Getwd()
	if err != nil {
		log.Printf("[ANSIBLE] Failed to find path: %v", err)
		return nil,err
	}
	macAsID := strings.ReplaceAll(macAddress, ":", "-")
    filePattern := "kubeconfig-"+macAsID+".yaml"
	kubeconfigDest := filepath.Join(cwd, "assets", "ansible", filePattern)

	var playbookYAML []byte
	extraVar := make(map[string]interface{})
	extraVar["ansible_become_pass"] = "password"
	switch templateMode { 
	case "k3s-master":
		chunk, err := os.ReadFile("templates/ansible/k3s-master.yaml")
		if err != nil {
			return nil,err
		}
		playbookYAML = chunk
		extraVar["ansible_host"] = targetIP
		extraVar["kubeconfig_dest"] = kubeconfigDest
		extraVar["mac_address"] = macAddress
	case "k3s-worker":
		chunk, err := os.ReadFile("templates/ansible/k3s-worker.yaml")
		if err != nil {
			return nil,err
		}
		k3sCred,err:= FetchToken(nodeObj.IP, nodeObj.Name, nodeObj.Mac)
		if err != nil {
			return nil, err
		}

		playbookYAML = chunk
		extraVar["target_mac"] = macAddress
		extraVar["server"] = k3sCred.Server
		extraVar["token"] = k3sCred.Token

	}

	log.Printf("[ANSIBLE] template %s file size %d", templateMode, len(playbookYAML))

	machine := template{
		machineType: "k3s",
	}
	log.Printf("[ANSIBLE] Ansible Template %s", machine.machineType)

	playbookFile, cleanupPB := writeTemp("playbook-*.yaml", playbookYAML, 0644)
	keyFile, cleanupKF := writeTemp("id_ed25519-*", sshKey, 0600)
	defer cleanupPB()
	defer cleanupKF()

	os.Setenv("ANSIBLE_HOST_KEY_CHECKING", "False")
	opts := &playbook.AnsiblePlaybookOptions{
		Inventory: targetIP+",",
		User: userName,
		PrivateKey: keyFile,
		SSHExtraArgs:  "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -tt",
		BecomeMethod: "sudo",
		ExtraVars: extraVar,
	}
	
	log.Printf("[ANSIBLE] Target path: %s", kubeconfigDest)

	// Essential Args
	if userName == "ubuntu" {
		opts.AddExtraVar("ansible_become_exe", "sudo.ws")
	}
	if userName == "centos" {
    	opts.AddExtraVar("k3s_extra_flags", "--write-kubeconfig-mode 644 --prefer-bundled-bin")
	} else {
	    opts.AddExtraVar("k3s_extra_flags", "--write-kubeconfig-mode 644")
	}

	cmd := playbook.NewAnsiblePlaybookCmd(
		playbook.WithPlaybooks(playbookFile),
		playbook.WithPlaybookOptions(opts),
	)

	var buf bytes.Buffer
	exec := stdoutcallback.NewJSONStdoutCallbackExecute(
		execute.NewDefaultExecute(
			execute.WithCmd(cmd),
			execute.WithWrite(&buf),
			execute.WithErrorEnrich(playbook.NewAnsiblePlaybookErrorEnrich()),
		),
	)

	res, err := ansiblejson.ParseJSONResultsStream(&buf)
	if err := exec.Execute(context.Background()); err != nil {
		log.Printf("[Ansible] Fail to execute ansible: %v", err)
		return res,err
	}
	return res,nil
}
