package httpapi

import (
	"mime/multipart"
	"net/http"
)

func (a *API) uploadMedia(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 11<<20)
	if err := r.ParseMultipartForm(11 << 20); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", "照片过大或上传格式错误")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", "请选择一张照片")
		return
	}
	defer file.Close()
	asset, err := a.service.UploadMedia(r.Context(), currentUser(r).ID, r.FormValue("kind"), header.Filename, multipartMIME(header), header.Size, file)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, asset)
}

func (a *API) createDemoMedia(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Kind string `json:"kind"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	asset, err := a.service.CreateDemoMedia(r.Context(), currentUser(r).ID, input.Kind)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, asset)
}

func multipartMIME(header *multipart.FileHeader) string {
	value := header.Header.Get("Content-Type")
	if value == "" {
		value = "application/octet-stream"
	}
	return value
}
