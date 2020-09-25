package celbuxhelpers

import (
	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	"cloud.google.com/go/datastore"
	"cloud.google.com/go/errorreporting"
	"cloud.google.com/go/logging"
	"cloud.google.com/go/storage"
	b64 "encoding/base64"
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
		return LogError(err)
	}
	return nil
}

// Decode request into provided struct pointer
func DecodeStruct(w http.ResponseWriter, r *http.Request, out interface{}) error {
	if r.Header.Get("Content-Type") != "" {
		value, _ := header.ParseValueAndParams(r.Header, "Content-Type")
		if value != "application/json" {
			return status.Error(codes.FailedPrecondition, "content type must be application/json")
		}
	}

	err := json.NewDecoder(r.Body).Decode(&out)
	if err != nil {
		fmt.Println(err.Error())
		return LogError(err)
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

func PrintHTTPBody(resp *http.Response) (string, error) {
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", LogError(err)
	}

	return string(body), nil
}

func StructToBase64(in interface{}) (string, error) {
	structJSON, err := json.Marshal(in)
	if err != nil {
	    return "", LogError(err)
	}

	return b64.URLEncoding.EncodeToString(structJSON), nil
}

func Base64ToStruct(base64 string, out interface{}) error {
	structJSON, err := b64.URLEncoding.DecodeString(base64)
	if err != nil {
		return LogError(err)
	}

	err = json.Unmarshal(structJSON, &out)
	if err != nil {
		return LogError(err)
	}

	return nil
}