package restapi

import "encoding/json"

// The two profile lists are components/responses whose body is a bare array.
// oapi-codegen wraps such a response in a struct embedding the array type, and
// encoding/json marshals an embedded non-struct as a named field, so without
// these the list would answer {"MusicQualityProfileListJSONResponse":[...]}
// instead of [...].

func (r ListMusicQualityProfiles200JSONResponse) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.MusicQualityProfileListJSONResponse)
}

func (r ListBookQualityProfiles200JSONResponse) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.BookQualityProfileListJSONResponse)
}
