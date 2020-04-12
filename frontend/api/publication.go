// Copyright (c) 2016 Readium Foundation
//
// Redistribution and use in source and binary forms, with or without modification,
// are permitted provided that the following conditions are met:
// Copyright 2017 European Digital Reading Lab. All rights reserved.
// Licensed to the Readium Foundation under one or more contributor license agreements.
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE file exposed on Github (readium) in the project repository.

package staticapi

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/readium/readium-lcp-server/api"
	"github.com/readium/readium-lcp-server/frontend/webpublication"
	"github.com/readium/readium-lcp-server/problem"
)

// GetPublications returns a list of publications
func GetPublications(w http.ResponseWriter, r *http.Request, s IServer) {
	var page int64
	var perPage int64
	var err error

	if r.FormValue("page") != "" {
		page, err = strconv.ParseInt((r).FormValue("page"), 10, 32)
		if err != nil {
			problem.Error(w, r, problem.Problem{Detail: err.Error()}, http.StatusBadRequest)
			return
		}
	} else {
		page = 1
	}

	if r.FormValue("per_page") != "" {
		perPage, err = strconv.ParseInt((r).FormValue("per_page"), 10, 32)
		if err != nil {
			problem.Error(w, r, problem.Problem{Detail: err.Error()}, http.StatusBadRequest)
			return
		}
	} else {
		perPage = 100
	}

	if page > 0 {
		page-- //pagenum starting at 0 in code, but user interface starting at 1
	}

	if page < 0 {
		problem.Error(w, r, problem.Problem{Detail: "page must be positive integer"}, http.StatusBadRequest)
		return
	}

	pubs := make([]webpublication.Publication, 0)
	fn := s.PublicationAPI().List(int(perPage), int(page))
	for it, err := fn(); err == nil; it, err = fn() {
		pubs = append(pubs, it)
	}
	if len(pubs) > 0 {
		nextPage := strconv.Itoa(int(page) + 1)
		w.Header().Set("Link", "</publications/?page="+nextPage+">; rel=\"next\"; title=\"next\"")
	}
	if page > 1 {
		previousPage := strconv.Itoa(int(page) - 1)
		w.Header().Set("Link", "</publications/?page="+previousPage+">; rel=\"previous\"; title=\"previous\"")
	}
	w.Header().Set("Content-Type", api.ContentType_JSON)

	enc := json.NewEncoder(w)
	err = enc.Encode(pubs)
	if err != nil {
		problem.Error(w, r, problem.Problem{Detail: err.Error()}, http.StatusBadRequest)
		return
	}
}

// GetPublication returns a publication from its numeric id, given as part of the calling url
//
func GetPublication(w http.ResponseWriter, r *http.Request, s IServer) {
	vars := mux.Vars(r)
	var id int
	var err error
	if id, err = strconv.Atoi(vars["id"]); err != nil {
		// id is not a number
		problem.Error(w, r, problem.Problem{Detail: "The publication id must be an integer"}, http.StatusBadRequest)
	}

	if pub, err := s.PublicationAPI().Get(int64(id)); err == nil {
		enc := json.NewEncoder(w)
		if err = enc.Encode(pub); err == nil {
			// send a json serialization of the publication
			w.Header().Set("Content-Type", api.ContentType_JSON)
			return
		}
		problem.Error(w, r, problem.Problem{Detail: err.Error()}, http.StatusInternalServerError)
	} else {
		switch err {
		case webpublication.ErrNotFound:
			{
				problem.Error(w, r, problem.Problem{Detail: err.Error()}, http.StatusNotFound)
			}
		default:
			{
				problem.Error(w, r, problem.Problem{Detail: err.Error()}, http.StatusInternalServerError)
			}
		}
	}
}

// CheckPublicationByTitle check if a publication with this title exist
func CheckPublicationByTitle(w http.ResponseWriter, r *http.Request, s IServer) {
	var title string
	title = r.URL.Query()["title"][0]

	log.Println("Check publication stored with name " + string(title))

	if pub, err := s.PublicationAPI().CheckByTitle(string(title)); err == nil {
		enc := json.NewEncoder(w)
		if err = enc.Encode(pub); err == nil {
			// send a json serialization of the boolean response
			w.Header().Set("Content-Type", api.ContentType_JSON)
			return
		}
		problem.Error(w, r, problem.Problem{Detail: err.Error()}, http.StatusInternalServerError)
	} else {
		switch err {
		case webpublication.ErrNotFound:
			{
				log.Println("No publication stored with name " + string(title))
				//	problem.Error(w, r, problem.Problem{Detail: err.Error()}, http.StatusNotFound)
			}
		default:
			{
				problem.Error(w, r, problem.Problem{Detail: err.Error()}, http.StatusInternalServerError)
			}
		}
	}
}

//DecodeJSONPublication transforms a json string to a User struct
func DecodeJSONPublication(r *http.Request) (webpublication.Publication, error) {
	var dec *json.Decoder
	if ctype := r.Header["Content-Type"]; len(ctype) > 0 && ctype[0] == api.ContentType_JSON {
		dec = json.NewDecoder(r.Body)
	}
	pub := webpublication.Publication{}
	err := dec.Decode(&pub)
	return pub, err
}

// CreatePublication creates a publication in the database
func CreatePublication(w http.ResponseWriter, r *http.Request, s IServer) {
	var pub webpublication.Publication
	var err error
	if pub, err = DecodeJSONPublication(r); err != nil {
		problem.Error(w, r, problem.Problem{Detail: "incorrect JSON Publication " + err.Error()}, http.StatusBadRequest)
		return
	}

	// add publication
	if err := s.PublicationAPI().Add(pub); err != nil {
		problem.Error(w, r, problem.Problem{Detail: err.Error()}, http.StatusBadRequest)
		return
	}

	// publication added to db
	w.WriteHeader(http.StatusCreated)
}

// UploadPublication creates a new publication via a POST request
func UploadPublication(w http.ResponseWriter, r *http.Request, s IServer) {
	var pub webpublication.Publication
	pub.Title = r.URL.Query()["title"][0]
	s.PublicationAPI().Upload(r, w, pub)
}

// UpdatePublication updates an identified publication (id) in the database
func UpdatePublication(w http.ResponseWriter, r *http.Request, s IServer) {
	vars := mux.Vars(r)
	var id int
	var err error
	var pub webpublication.Publication
	if id, err = strconv.Atoi(vars["id"]); err != nil {
		// id is not a number
		problem.Error(w, r, problem.Problem{Detail: "Plublication ID must be an integer"}, http.StatusBadRequest)
		return
	}
	// ID is a number, check publication (json)
	if pub, err = DecodeJSONPublication(r); err != nil {
		problem.Error(w, r, problem.Problem{Detail: err.Error()}, http.StatusBadRequest)
		return
	}
	// publication ok, id is a number, search publication to update
	if foundPub, err := s.PublicationAPI().Get(int64(id)); err != nil {
		switch err {
		case webpublication.ErrNotFound:
			problem.Error(w, r, problem.Problem{Detail: err.Error()}, http.StatusNotFound)
		default:
			problem.Error(w, r, problem.Problem{Detail: err.Error()}, http.StatusInternalServerError)
		}
	} else {
		// publication is found!
		if err := s.PublicationAPI().Update(webpublication.Publication{
			ID:     foundPub.ID,
			Title:  pub.Title,
			Status: foundPub.Status}); err != nil {
			//update failed!
			problem.Error(w, r, problem.Problem{Detail: err.Error()}, http.StatusInternalServerError)
		}
		//database update ok
		w.WriteHeader(http.StatusOK)
		//return
	}
}

// DeletePublication removes a publication in the database
func DeletePublication(w http.ResponseWriter, r *http.Request, s IServer) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		problem.Error(w, r, problem.Problem{Detail: err.Error()}, http.StatusBadRequest)
		return
	}
	if err := s.PublicationAPI().Delete(id); err != nil {
		problem.Error(w, r, problem.Problem{Detail: err.Error()}, http.StatusBadRequest)
		return
	}
	// publication deleted from db
	w.WriteHeader(http.StatusOK)
}
