package http_handler

import "context"
import "log"
import "time"
import "net/http"
import "encoding/json"
import "github.com/motangpuar/o2-ims-worker/internal/db"
import "github.com/motangpuar/o2-ims-worker/internal/inventory"
import "github.com/motangpuar/o2-ims-worker/internal/ansible"
import "github.com/motangpuar/o2-ims-worker/internal/kubernetes"
import "github.com/motangpuar/o2-ims-worker/internal/config"
import "slices"

type HTTPError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

type pipeLine struct {
	Name string `json:"name"`
	Mac  string `json:"mac"`
	IP   string `json:"ip"`
	OS   string `json:"os"`
}

var httpContext *context.Context

func handleTest(w http.ResponseWriter, r *http.Request) {
	log.Printf("[HTTP] Request arrive for Test Path...")
	w.Write([]byte("This is the test path...\n"))
} 

func logFileServer(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[HTTP] Request for file: %s", r.URL.Path)
		next.ServeHTTP(w, r)
	}
}

func handleInventory(w http.ResponseWriter, r *http.Request){
	log.Printf("[HTTP] Request for inventories")
	inventories := inventory.FetchMachines()
	
	err := json.NewEncoder(w).Encode(inventories)
	if err != nil {
		log.Printf("[HTTP] Error...")
		return
	}
	log.Printf("[HTTP] Inventories, %v", inventories)
}

func handlePipeline(w http.ResponseWriter, r *http.Request) {
	fd := filedata.Gather() 
	responseBody := fd.Clients

	switch response := r.Method; response {
	case http.MethodPost:
		defer r.Body.Close()
		var pipe pipeLine 
		err := json.NewDecoder(r.Body).Decode(&pipe)

		if err != nil {
			log.Printf("Broken Format !!!!")
			http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		
		log.Printf("[HTTP] Received data %+v", pipe)
		validOSTypes := []string{"debian", "centos", "ubuntu"}
		if  !slices.Contains(validOSTypes, pipe.OS) {
			log.Printf("No OS Type like that")
			http.Error(w, "OS Type Not Exist: "+pipe.OS+". Only valid options are (debian, centos, ubuntu)", http.StatusBadRequest)
			return
		}

		if responseBody[pipe.Mac] != nil {
			log.Printf("[HTTP] Entry Exist")
			http.Error(w, "Entry Exist: "+pipe.Mac, http.StatusBadRequest)
			return
		} else {
			for _,c := range responseBody{
				if c.OfferIP() == pipe.IP {
					log.Printf("[HTTP] IP Exist")
					http.Error(w, "IP Exist: "+pipe.IP, http.StatusBadRequest)
					return
				}
			}
		}
		secret := config.Gather().General.GetSecret()
		log.Printf("[HTTP] Secret: %s", secret)
		filedata.AddItemToFile(pipe.IP, pipe.Mac, pipe.OS, secret)
	case http.MethodGet:
		log.Printf("[HTTP] Request for pipeline: %s", r.URL.Path)
		jsonPayload := make(map[string]any)

		for m,c := range responseBody {
			jsonPayload[m] = c.ToMap()
		}
		
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(jsonPayload)
		if err != nil {
			log.Printf("Failed to send Response")
			http.Error(w, "Error Detected", http.StatusInternalServerError)
			return
		}
	default:
		log.Printf("[HTTP] Bad Request: %s", r.URL.Path)
		return
	}
}

func handleCallback(w http.ResponseWriter, r *http.Request){
	log.Printf("[HTTP] Request arrive for Callback...")

	queryParams := r.URL.Query()
	macQuery := queryParams.Get("mac")

	if macQuery == "" {
		log.Printf("[HTTP] Machines %v", inventory.FetchMachines())
	} else {
		err := inventory.LogInstalledMachine(macQuery, "", time.Now())
		if err != nil {
			http.Error(w, "Error "+err.Error(), http.StatusBadRequest)
		} else {
			w.Write([]byte("Success!"))
		}
	}

}

func handleAnsibleFetchToken(w http.ResponseWriter, r *http.Request){
	log.Printf("[HTTP] Request arrive for Ansible")

	queryParams := r.URL.Query()
	macQuery := queryParams.Get("mac")

	if macQuery == "" {
		log.Printf("[HTTP] Ansible Options")
	} else {
		value, exist := filedata.Gather().Clients[macQuery]

		if value == nil {
			log.Printf("Failed to send Response")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError) // 500 Status Code
			json.NewEncoder(w).Encode(HTTPError{
				Error:   "InternalServerError",
				Message: "Object not exist",
			})
			return
		}

		log.Printf("[HTTP] %v is %v", value, exist)

		username := value.OSType()
		template := value.GetTemplate()

		log.Printf("[HTTP] Template is %s",template)
		if exist != true {
			return
		}

		token,err := ansible_worker.FetchToken(value.OfferIP(), username, macQuery)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError) // 500 Status Code
			json.NewEncoder(w).Encode(HTTPError{
				Error:   "InternalServerError",
				Message: err.Error(),
			})
			return

		}
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(token)
		if err != nil {
			log.Printf("Failed to send Response")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError) // 500 Status Code
			json.NewEncoder(w).Encode(HTTPError{
				Error:   "InternalServerError",
				Message: err.Error(),
			})
			return
		}
	}
}

func handleAnsible(w http.ResponseWriter, r *http.Request){
	log.Printf("[HTTP] Request arrive for Ansible")
	queryParams := r.URL.Query()
	macQuery := queryParams.Get("mac")

	if macQuery == "" {
		log.Printf("[HTTP] Ansible Options")
	} else {
		clients := filedata.Gather().Clients
		value,exist := clients[macQuery]
		machines := value.GetMachines()

		log.Printf("-> %v", machines) 

		if value == nil {
			log.Printf("Failed to send Response")
			http.Error(w, "Object not exist ", http.StatusInternalServerError)
			return
		}

		log.Printf("[HTTP] %v is %v", value, exist)

		//ansibleInfo := ansible_worker.GetInfo()
		username := value.OSType()
		template := value.GetTemplate()
		cluster := value.GetCluster()
		role := value.GetMachines().Role

		// This should be worker specific
		var nodeObj struct {
		    Name string
		    IP   string
		    Mac  string
		    Role string
		}
		if role != "master" {
			clusterDump,err := kubeclient.GetClusters(*httpContext)

			if err != nil {
				log.Printf("[HTTP] Cluster is empty")
				http.Error(w, "Cluster is broken: "+err.Error(), http.StatusInternalServerError)
			}

			nodes := clusterDump.Cluster[cluster].Info.Nodes
			for _,n := range(nodes) {
				log.Printf("Process Node %s", n.Name)
				for _, r := range(n.Roles) {
					if r == "control-plane" {
						var dumpMac string
						var dumpName string
						for _, m := range(clients){
							// Find Mac and IP of this shit
							if m.GetMachines().IP == n.InternalIP {
								dumpName = m.GetMachines().OSType
								dumpMac = m.MACAddress()
							}
						}
						nodeObj = struct {
							Name string
							IP string
							Mac string
							Role string
						}{
							Name: dumpName,
							IP: n.InternalIP,
							Mac: dumpMac,
							Role: r,
						}
					}
				}
			}
		} else {
			nodeObj = struct {
				Name string
				IP string
				Mac string
				Role string
			}{
				Name: "",
				IP: "",
				Mac: "",
				Role: "",
			}
		}

		log.Printf("[HTTP] Template is %s ",template)
		if exist != true {
			return
		}

		exec,err := ansible_worker.Populate(value.OfferIP(), macQuery, username, template, nodeObj)
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(exec)
		if err != nil {
			http.Error(w, "Exec Error"+err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func handleKubernetes(w http.ResponseWriter, r *http.Request){


	// Should iterate over multiple kubeconfigs
	// for different clusters. Fetch clusters here.
	// Clusters states modified only based on 
	// ansible's actions.

	queryParams := r.URL.Query()
	macQuery := queryParams.Get("item")

	if macQuery == "clusters" {
		responseBody,err := kubeclient.GetClusters(*httpContext)
		if err != nil {
			log.Printf("Failed to send Response")
			http.Error(w, "Error Detected"+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(responseBody)
		if err != nil {
			log.Printf("Failed to send Response")
			http.Error(w, "Error Detected", http.StatusInternalServerError)
			return
		}
		return
	}

	info,err := kubeclient.GetClusters(*httpContext)
	
	var payload []any
	responseBody := append(payload, info)
	
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(responseBody)
	if err != nil {
		log.Printf("Failed to send Response")
		http.Error(w, "Error Detected", http.StatusInternalServerError)
		return
	}
}

func Serve(ctx context.Context) {
	// Populate global context
	httpContext = &ctx

	dir := "assets/http/"
	mux := http.NewServeMux()
	filehandler := http.StripPrefix("/", http.FileServer(http.Dir(dir)))
	mux.HandleFunc("/", logFileServer(filehandler))
	mux.HandleFunc("/pipeline", handlePipeline)
	mux.HandleFunc("/test", handleTest)
	mux.HandleFunc("/inventories", handleInventory)
	mux.HandleFunc("/callback", handleCallback)
	mux.HandleFunc("/ansible", handleAnsible)
	mux.HandleFunc("/ansible-token", handleAnsibleFetchToken)
	mux.HandleFunc("/kubernetes", handleKubernetes)
	log.Fatal(http.ListenAndServe(":8033", mux))
}
