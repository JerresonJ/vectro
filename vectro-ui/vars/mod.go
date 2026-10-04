package vars

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	VectorResources = "/anki/data/assets/cozmo_resources/"
	WiredData       = "/data/wired/mods/"
)

type Modification interface {
	Name() string
	Description() string
	HTTP(w http.ResponseWriter, r *http.Request)
	Load() error
}

var EnabledMods []Modification

func IsEndpoint(r *http.Request, endpoint string) bool {
	return strings.Contains(r.URL.Path, endpoint)
}

func FindMod(name string) (Modification, error) {
	for index, mod := range EnabledMods {
		if strings.TrimSpace(name) == mod.Name() {
			return EnabledMods[index], nil
		}
	}
	return nil, errors.New("mod not found")
}

func GetModDir(modname string) string {
	return filepath.Join(WiredData, modname)
}

func SaveFile(contents string, path string) error {
	os.MkdirAll(filepath.Dir(path), 0777)
	return os.WriteFile(path, []byte(contents), 0777)
}

func ReadFile(path string) (contents string, err error) {
	out, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func SetAnkiPerms() {

}

func ExtraHTTP(w http.ResponseWriter, r *http.Request) {
	if IsEndpoint(r, "restartvic") {
		RestartVic()
		HTTPSuccess(w, r)
	} else {
		HTTPError(w, r, "not found")
	}
}

func InitMods() {
	for _, mod := range EnabledMods {
		fmt.Println("Loading " + mod.Name() + "...")
		err := mod.Load()
		if err != nil {
			fmt.Println("ERROR loading", mod.Name(), ":", err)
			continue
		}
		http.HandleFunc("/api/mods/"+mod.Name()+"/", mod.HTTP)
	}
	http.HandleFunc("/api/extra/", ExtraHTTP)
}

// robotUnits are stopped explicitly: stopping anki-robot.target alone can
// leave vic-anim running, disconnected from the new engine (fault 915/917).
const robotUnits = "vic-cloud vic-engine vic-anim vic-robot vic-switchboard"

func StopVic() {
	exec.Command("/bin/sh", "-c", "systemctl stop anki-robot.target; systemctl stop "+robotUnits).Run()
	time.Sleep(2 * time.Second)
}

func StartVic() {
	exec.Command("/bin/sh", "-c", "systemctl start anki-robot.target").Run()
	time.Sleep(3 * time.Second)
}

func RestartVic() {
	StopVic()
	StartVic()
}

type HTTPStatus struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func HTTPSuccess(w http.ResponseWriter, r *http.Request) {
	var status HTTPStatus
	status.Status = "success"
	successBytes, _ := json.Marshal(status)
	w.Write(successBytes)
}

func HTTPError(w http.ResponseWriter, r *http.Request, err string) {
	var status HTTPStatus
	status.Status = "error"
	status.Message = err
	errorBytes, _ := json.Marshal(status)
	w.WriteHeader(500)
	w.Write(errorBytes)
}

// type BehaviorMessage struct {
// 	Type   string `json:"type"`
// 	Module string `json:"module"`
// 	Data   struct {
// 		BehaviorName     string `json:"behaviorName"`
// 		PresetConditions bool   `json:"presetConditions"`
// 	} `json:"data"`
// }

// //{"type":"data","module":"behaviors","data":{"behaviorName":"DevBaseBehavior","presetConditions":false}}

// func Behavior(behavior string) {
// 	u := url.URL{Scheme: "ws", Host: "localhost:8888", Path: "/socket"}

// 	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
// 	if err != nil {
// 		fmt.Println("dial:", err)
// 		return
// 	}
// 	defer c.Close()

// 	message := BehaviorMessage{
// 		Type:   "data",
// 		Module: "behaviors",
// 		Data: struct {
// 			BehaviorName     string `json:"behaviorName"`
// 			PresetConditions bool   `json:"presetConditions"`
// 		}{
// 			BehaviorName:     behavior,
// 			PresetConditions: false,
// 		},
// 	}

// 	marshaledMessage, err := json.Marshal(message)
// 	if err != nil {
// 		log.Fatal("marshal:", err)
// 	}

// 	err = c.WriteMessage(websocket.TextMessage, marshaledMessage)
// 	if err != nil {
// 		log.Fatal("write:", err)
// 	}
// }
