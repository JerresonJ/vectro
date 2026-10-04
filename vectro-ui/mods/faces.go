package mods

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/digital-dream-labs/vector-go-sdk/pkg/vectorpb"
	"vectro/ui/vars"
)

type Faces struct {
	vars.Modification
}

func NewFaces() *Faces {
	return &Faces{}
}

func (modu *Faces) Name() string {
	return "Faces"
}

func (modu *Faces) Description() string {
	return "Allows the user to add a new face, modify faces, delete faces"
}

func (modu *Faces) Load() error {
	// jdoc-settings creates a ctx, we are using that
	return nil
}

func (m *Faces) HTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/api/mods/"+m.Name()) {
		return
	}
	switch strings.TrimPrefix(r.URL.Path, "/api/mods/"+m.Name()+"/") {
	case "getFaces":
		list, err := getFaces()
		if err != nil {
			vars.HTTPError(w, r, err.Error())
			return
		}
		out, err := json.Marshal(list)
		if err != nil {
			vars.HTTPError(w, r, err.Error())
			return
		}
		w.Write(out)
		return
	case "deleteFace":
		id := r.FormValue("id")
		if id == "" {
			vars.HTTPError(w, r, "empty id")
			return
		}
		i, err := strconv.Atoi(id)
		if err != nil {
			vars.HTTPError(w, r, "id contains invalid characters")
			return
		}
		err = deleteFace(int32(i))
		if err != nil {
			vars.HTTPError(w, r, err.Error())
			return
		}
	case "renameFace":
		id := r.FormValue("id")
		newName := r.FormValue("name")
		if id == "" {
			vars.HTTPError(w, r, "empty id")
			return
		}
		if newName == "" {
			vars.HTTPError(w, r, "empty name")
			return
		}
		i, err := strconv.Atoi(id)
		if err != nil {
			vars.HTTPError(w, r, "id contains invalid characters")
			return
		}
		err = renameFace(int32(i), newName)
		if err != nil {
			vars.HTTPError(w, r, err.Error())
			return
		}
	case "trainFace":
		name := r.FormValue("name")
		if name == "" {
			vars.HTTPError(w, r, "empty name")
			return
		}
		err := trainFace(name)
		if err != nil {
			vars.HTTPError(w, r, "empty name")
			return
		}
	default:
		vars.HTTPError(w, r, "404 not found")
		return
	}
	vars.HTTPSuccess(w, r)
}

type EnrolledFace struct {
	ID                        int32  `json:"id"`
	Name                      string `json:"name"`
	SecondsSinceFirstEnrolled int64  `json:"secondssincefirstenrolled"`
}

func getFaces() ([]EnrolledFace, error) {
	v, err := vars.GetVec()
	if err != nil {
		return []EnrolledFace{}, err
	}
	resp, err := v.Conn.RequestEnrolledNames(ctx, &vectorpb.RequestEnrolledNamesRequest{})
	if err != nil {
		return []EnrolledFace{}, err
	}
	var faces []EnrolledFace
	for _, r := range resp.Faces {
		var face EnrolledFace
		face.ID = r.FaceId
		face.Name = r.Name
		face.SecondsSinceFirstEnrolled = r.SecondsSinceFirstEnrolled
		faces = append(faces, face)
	}
	return faces, nil
}

func deleteFace(id int32) error {
	v, err := vars.GetVec()
	if err != nil {
		return err
	}
	_, err = v.Conn.EraseEnrolledFaceByID(ctx, &vectorpb.EraseEnrolledFaceByIDRequest{
		FaceId: id,
	})
	return err
}

func renameFace(id int32, newName string) error {
	currentFaces, err := getFaces()
	if err != nil {
		return err
	}
	var oldName string
	var matched bool
	for _, r := range currentFaces {
		if r.ID == id {
			oldName = r.Name
			matched = true
		}
	}
	if !matched {
		// not doing this with `oldName == ""` because it is possible for an empty name to exist
		return errors.New("face id does not exist")
	}
	v, err := vars.GetVec()
	if err != nil {
		return err
	}
	_, err = v.Conn.UpdateEnrolledFaceByID(ctx,
		&vectorpb.UpdateEnrolledFaceByIDRequest{
			FaceId:  id,
			OldName: oldName,
			NewName: newName,
		})
	return err
}

func trainFace(name string) error {
	currentFaces, err := getFaces()
	if err != nil {
		return err
	}
	for _, r := range currentFaces {
		if strings.EqualFold(r.Name, name) {
			return errors.New("face name already exists")
		}
	}
	v, err := vars.GetVec()
	if err != nil {
		return err
	}
	if _, err = v.Conn.SetFaceToEnroll(ctx, &vectorpb.SetFaceToEnrollRequest{
		Name:        name,
		SaveToRobot: true,
		SayName:     true,
	}); err != nil {
		return err
	}
	// SetFaceToEnroll only queues the name; the app then asked the engine
	// to run its "meet Victor" behavior, which does the actual enrollment.
	_, err = v.Conn.AppIntent(ctx, &vectorpb.AppIntentRequest{Intent: "intent_meet_victor", Param: name})
	return err
}
