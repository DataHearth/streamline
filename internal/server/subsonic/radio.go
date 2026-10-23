package subsonic

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/radiostation"
)

// webURL accepts an absolute http(s) URL. Streamline never fetches a
// station: this only keeps a client from being handed a javascript: or file:
// link.
func webURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	return raw, true
}

func badURL(name string) error {
	return &apiError{
		Code:    errGeneric,
		Message: name + " must be an http or https URL",
	}
}

func toStation(s *ent.RadioStation) radioStation {
	return radioStation{
		ID:          radioID(s.ID),
		Name:        s.Name,
		StreamURL:   s.StreamURL,
		HomePageURL: s.HomepageURL,
	}
}

func (h *Handler) getInternetRadioStations(
	w http.ResponseWriter,
	r *http.Request,
	_ *ent.User,
) error {
	ss, err := h.ent.RadioStation.Query().
		Order(radiostation.ByName(), radiostation.ByID()).
		All(r.Context())
	if err != nil {
		return err
	}
	out := &radioStations{Station: make([]radioStation, 0, len(ss))}
	for _, s := range ss {
		out.Station = append(out.Station, toStation(s))
	}
	writeOK(w, r, &body{InternetRadioStations: out})
	return nil
}

// stationFields reads name, streamUrl and homepageUrl; required makes the
// first two mandatory (create), otherwise each is applied only when sent.
type stationFields struct {
	name, stream, homepage *string
}

func readStation(r *http.Request, required bool) (stationFields, error) {
	var f stationFields
	if _, ok := r.Form["name"]; ok || required {
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			return f, missingParam("name")
		}
		f.name = &name
	}
	if _, ok := r.Form["streamUrl"]; ok || required {
		if r.FormValue("streamUrl") == "" {
			return f, missingParam("streamUrl")
		}
		s, ok := webURL(r.FormValue("streamUrl"))
		if !ok {
			return f, badURL("streamUrl")
		}
		f.stream = &s
	}
	if _, ok := r.Form["homepageUrl"]; ok {
		hp := ""
		if raw := strings.TrimSpace(r.FormValue("homepageUrl")); raw != "" {
			s, ok := webURL(raw)
			if !ok {
				return f, badURL("homepageUrl")
			}
			hp = s
		}
		f.homepage = &hp
	}
	return f, nil
}

// The station list is shared by every user, so changing it is an admin's.
func (h *Handler) createInternetRadioStation(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	if !isAdmin(u) {
		return errNotAuthorizedErr
	}
	f, err := readStation(r, true)
	if err != nil {
		return err
	}
	create := h.ent.RadioStation.Create().SetName(*f.name).SetStreamURL(*f.stream)
	if f.homepage != nil {
		create.SetHomepageURL(*f.homepage)
	}
	if err := create.Exec(r.Context()); err != nil {
		return err
	}
	writeOK(w, r, nil)
	return nil
}

func (h *Handler) updateInternetRadioStation(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	if !isAdmin(u) {
		return errNotAuthorizedErr
	}
	id, err := idParam(r, kindRadio)
	if err != nil {
		return err
	}
	f, err := readStation(r, false)
	if err != nil {
		return err
	}
	up := h.ent.RadioStation.UpdateOneID(id)
	if f.name != nil {
		up.SetName(*f.name)
	}
	if f.stream != nil {
		up.SetStreamURL(*f.stream)
	}
	if f.homepage != nil {
		up.SetHomepageURL(*f.homepage)
	}
	if err := up.Exec(r.Context()); err != nil {
		return lookupErr(err)
	}
	writeOK(w, r, nil)
	return nil
}

func (h *Handler) deleteInternetRadioStation(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	if !isAdmin(u) {
		return errNotAuthorizedErr
	}
	id, err := idParam(r, kindRadio)
	if err != nil {
		return err
	}
	if err := h.ent.RadioStation.DeleteOneID(id).Exec(r.Context()); err != nil {
		return lookupErr(err)
	}
	writeOK(w, r, nil)
	return nil
}
