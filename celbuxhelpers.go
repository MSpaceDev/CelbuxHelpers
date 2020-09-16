package celbuxhelpers

import (
	"bytes"
	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	"cloud.google.com/go/datastore"
	"cloud.google.com/go/errorreporting"
	"cloud.google.com/go/logging"
	"cloud.google.com/go/storage"
	"encoding/json"
	"fmt"
	"github.com/golang/gddo/httputil/header"
	"golang.org/x/net/context"
	taskspb "google.golang.org/genproto/googleapis/cloud/tasks/v2"
	ltype "google.golang.org/genproto/googleapis/logging/type"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io/ioutil"
	"log"
	"net/http"
	"os"
)

/* GCP Helpers
 * Celbux helpers for easy & neat integration with GCP in main app engine code
 * Requirement: Call initialiseClients(projectID) in main app start up
 */
var ErrorClient *errorreporting.Client
var DatastoreClient *datastore.Client
var StorageClient *storage.Client
var LoggingClient *logging.Client
var TasksClient *cloudtasks.Client

func GetProjectID() (string, error) {
	// Get Project ID
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		return "", status.Error(codes.NotFound, "env var GOOGLE_CLOUD_PROJECT must be set")
	}

	return projectID, nil
}

//IntialiseClients provides all required GCP clients for use in main app engine code
func IntialiseClients(projectID string) error {
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
			return LogError(err)
		}
	}

	// Creates logging client
	if LoggingClient == nil {
		LoggingClient, err = logging.NewClient(context.Background(), projectID)
		if err != nil {
			return LogError(err)
		}
	}

	// Creates storage client
	if StorageClient == nil {
		StorageClient, err = storage.NewClient(context.Background())
		if err != nil {
			return LogError(err)
		}
	}

	// Creates storage client
	if TasksClient == nil {
		TasksClient, err = cloudtasks.NewClient(context.Background())
		if err != nil {
			return LogError(err)
		}
	}

	return nil
}

// Writes the encoded marshalled json into the http writer mainly for the purpose of a response
func EncodeStruct(w http.ResponseWriter, obj interface{}) error {
	(w).Header().Set("Content-Type", "application/json; charset=utf-8")
	err := json.NewEncoder(w).Encode(obj)
	if err != nil {
		fmt.Println(err.Error())
		return err
	}
	return nil
}

// Decode request into provided struct pointer
func DecodeStruct(w http.ResponseWriter, r *http.Request, obj interface{}) error {
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
func GLog(name string, text string, severity *ltype.LogSeverity) {
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

func LogError(err error) error {
	// Log for Logs Viewer
	ErrorClient.Report(errorreporting.Entry{
		Error: err,
	})

	// Log for Local
	fmt.Printf("Error: %v", err.Error())

	// Optional for quick-hand returns in other func
	return err
}

//DownloadObject downloads an object from Cloud Storage
func DownloadObject(bucket string, object string) ([]byte, error) {
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

// createHTTPTask creates a new task with a HTTP target then adds it to a Queue.
// e.g. projects/bulk-writes/locations/europe-west1/queues/datastore-queue
func QueueHTTPRequest(projectID, locationID, queueID string, request *taskspb.HttpRequest) (*taskspb.Task, error) {
	// Build the Task queue path.
	queuePath := fmt.Sprintf("projects/%s/locations/%s/queues/%s", projectID, locationID, queueID)

	// Build the Task payload.
	// https://godoc.org/google.golang.org/genproto/googleapis/cloud/tasks/v2#CreateTaskRequest
	req := &taskspb.CreateTaskRequest{
		Parent: queuePath,
		Task: &taskspb.Task{
			// https://godoc.org/google.golang.org/genproto/googleapis/cloud/tasks/v2#HttpRequest
			MessageType: &taskspb.Task_HttpRequest{
				HttpRequest: request,
			},
		},
	}

	createdTask, err := TasksClient.CreateTask(context.Background(), req)
	if err != nil {
		return nil, LogError(err)
	}

	return createdTask, nil
}

// Used both for receiving data here, and sending to queue service
type QueueServiceRequest struct {
	Kind string
	Entities []interface{}
}

// Properly splits up entities into 31MB chunks to be sent to queue-service coordinate writes
// App Engine HTTP PUT limit is 32MB.
func WriteToDatastore(request QueueServiceRequest) error {
	queueServiceRequest := QueueServiceRequest{
		Kind: request.Kind,
		Entities: nil,
	}

	var inOperation bool
	var bits int
	for _, entity := range request.Entities {
		// Set to true when operating
		inOperation = true

		// Get megabytes
		bits += len(entity.([]byte))
		megabytes := bits / 8000000

		// If data is over 31 megabytes, send HTTP request, else just add entity to slice
		if megabytes >= 31 {
			err := sendRequest(queueServiceRequest)
			if err != nil {
				return LogError(err)
			}

			inOperation = false
			queueServiceRequest.Entities = nil
		} else {
			queueServiceRequest.Entities = append(queueServiceRequest.Entities, entity)
		}
	}

	// Makes sure to write last data if for loop exited while still in operation
	if inOperation {
		err := sendRequest(queueServiceRequest)
		if err != nil {
			return LogError(err)
		}

		inOperation = false
	}

	return nil
}

func sendRequest(data QueueServiceRequest) error {
	client := &http.Client{}
	projectID, err := GetProjectID()
	if err != nil {
		return LogError(err)
	}

	var dataJSON []byte
	dataJSON, err = json.Marshal(data)
	if err != nil {
		return LogError(err)
	}

	var req *http.Request
	req, err = http.NewRequest(http.MethodPut, fmt.Sprintf("queue-service-dot-%v.ew.r.appspot.com/start_work?opsPerInstance=1&entitiesPerRequest=500", projectID), bytes.NewBuffer(dataJSON))
	if err != nil {
		return LogError(err)
	}

	_, err = client.Do(req)
	if err != nil {
		return LogError(err)
	}

	return nil
}