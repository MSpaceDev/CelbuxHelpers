package celbuxhelpers

import (
	"cloud.google.com/go/datastore"
	"cloud.google.com/go/errorreporting"
	"cloud.google.com/go/logging"
	"cloud.google.com/go/storage"
	"encoding/json"
	"fmt"
	"github.com/golang/gddo/httputil/header"
	"golang.org/x/net/context"
	ltype "google.golang.org/genproto/googleapis/logging/type"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io/ioutil"
	"log"
	"net/http"
)

/* GCP Helpers
 * Celbux helpers for easy & neat integration with GCP in main app engine code
 * Requirement: Call initialiseClients(projectID) in main app start up
 */
var ErrorClient *errorreporting.Client
var DatastoreClient *datastore.Client
var StorageClient *storage.Client
var LoggingClient *logging.Client

//intialiseClients provides all required GCP clients for use in main app engine code
func intialiseClients(projectID string) error {
	// Initialise error to prevent shadowing
	var err error

	// Creates error client
	if ErrorClient == nil {
		ErrorClient, err = errorreporting.NewClient(context.Background(), projectID, errorreporting.Config{
			ServiceName: projectID + "-service",
			OnError: func(err error) {
				log.Printf("Could not log error: %v", err)
			},
		})
		if err != nil {
			log.Fatal(err)
		}
	}

	// Creates datastore client
	if DatastoreClient == nil {
		DatastoreClient, err = datastore.NewClient(context.Background(), projectID)
		if err != nil {
			ErrorClient.Report(errorreporting.Entry{Error: err})
			return err
		}
	}

	// Creates logging client
	if LoggingClient == nil {
		LoggingClient, err = logging.NewClient(context.Background(), projectID)
		if err != nil {
			ErrorClient.Report(errorreporting.Entry{Error: err})
			return err
		}
	}

	// Test error
	ErrorClient.Report(errorreporting.Entry{Error: err})

	// Creates storage client
	if StorageClient == nil {
		StorageClient, err = storage.NewClient(context.Background())
		if err != nil {
			ErrorClient.Report(errorreporting.Entry{Error: err})
			return err
		}
	}

	return nil
}

// Writes the encoded marshalled json into the http writer mainly for the purpose of a response
func encodeStruct(w *http.ResponseWriter, obj interface{}) error {
	(*w).Header().Set("Content-Type", "application/json; charset=utf-8")
	err := json.NewEncoder(*w).Encode(obj)
	if err != nil {
		fmt.Println(err.Error())
		return err
	}
	return nil
}

// Decode request into provided struct pointer
func decodeStruct(w http.ResponseWriter, r *http.Request, obj interface{}) error {
	if r.Header.Get("Content-Type") != "" {
		value, _ := header.ParseValueAndParams(r.Header, "Content-Type")
		if value != "application/json" {
			return status.Error(codes.FailedPrecondition, "content type must be application/json")
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	err := json.NewDecoder(r.Body).Decode(&obj)

	if err != nil {
		fmt.Println(err.Error())
		return err
	}

	return nil
}

//severity is nillable. Debug by default
func gLog(name string, text string, severity *ltype.LogSeverity) {
	// Sets log name to unix nano second
	logger := LoggingClient.Logger(name)

	// Set severity based on params. Default Severity: DEBUG
	var logSeverity logging.Severity
	if severity == nil {
		logSeverity = logging.Severity(ltype.LogSeverity_DEBUG)
	} else {
		logSeverity = logging.Severity(*severity)
	}

	// Adds an entry to the log buffer.
	logger.Log(logging.Entry{
		Payload: text,
		Severity: logSeverity,
	})
}

// downloadFile downloads an object from Cloud Storage
func downloadFile(bucket string, object string) ([]byte, error) {
	rc, err := StorageClient.Bucket(bucket).Object(object).NewReader(context.Background())
	if err != nil {
		return nil, fmt.Errorf("Object(%q).NewReader: %v", object, err)
	}
	defer rc.Close()

	data, err := ioutil.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("ioutil.ReadAll: %v", err)
	}

	return data, nil
}