package celbuxhelpers

import (
	"bytes"
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
	"google.golang.org/appengine"
	taskspb "google.golang.org/genproto/googleapis/cloud/tasks/v2"
	ltype "google.golang.org/genproto/googleapis/logging/type"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

func GetProjectID() (string, error) {
	// Get Project ID
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		return "", status.Error(codes.NotFound, "env var GOOGLE_CLOUD_PROJECT must be set")
	}

	return projectID, nil
}

func IntialiseClients(projectID string) error {
	//IntialiseClients provides all required GCP clients for use in main app engine code
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

func EncodeStruct(w http.ResponseWriter, obj interface{}) error {
	// Writes the encoded marshalled json into the http writer mainly for the purpose of a response
	(w).Header().Set("Content-Type", "application/json; charset=utf-8")
	err := json.NewEncoder(w).Encode(obj)
	if err != nil {
		fmt.Println(err.Error())
		return err
	}
	return nil
}

func DecodeStruct(w http.ResponseWriter, r *http.Request, obj interface{}) error {
	// Decode request into provided struct pointer
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

func GLog(name string, text string, severity *ltype.LogSeverity) {
	//severity is nillable. Debug by default
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

func DownloadObject(bucket string, object string) ([]byte, error) {
	//DownloadObject downloads an object from Cloud Storage
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

func QueueHTTPRequest(projectID, locationID, queueID string, request *taskspb.HttpRequest) (*taskspb.Task, error) {
	// createHTTPTask creates a new task with a HTTP target then adds it to a Queue.
	// e.g. projects/bulk-writes/locations/europe-west1/queues/datastore-queue

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

type QueueServiceRequest struct {
	// Used both for receiving data here, and sending to queue service
	Kind string
	Entities []interface{}
}

func WriteToDatastore(request QueueServiceRequest) error {
	// Properly splits up entities into 31MB chunks to be sent to queue-service coordinate writes
	// App Engine HTTP PUT limit is 32MB
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

func PrintHTTPBody(resp *http.Response) (string, error) {
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
	    return "", LogError(err)
	}
	return string(body), nil
}

func Encrypt(data string) string {
	return b64.URLEncoding.EncodeToString([]byte(data))
}

func Decrypt(data string) (string, error) {
	s, err := b64.URLEncoding.DecodeString(data)
	if err != nil {
		return "", err
	}
	return string(s), nil
}

func GetTestName() string {
	// Gets the current running method by reflection.
	// this is useful for linking tests to functions for logging.

	fpcs := make([]uintptr, 1)
	runtime.Callers(2, fpcs)
	caller := runtime.FuncForPC(fpcs[0] - 1)
	r := strings.Replace(caller.Name(), "github.com/MSpaceDev/JiraOnTheGO/src/service", "", -1)
	return strings.Replace(r, ".", "", -1)
}

func WriteFile(data string, name string) error {
	f, err := os.Create(name)
	if err != nil {
	    return err
	}
	defer f.Close()
	_, err = f.Write([]byte(data))
	if err != nil {
	    return err
	}
	return nil
}

func GetTimeString() string {
	loc,_ := time.LoadLocation("Africa/Johannesburg")
	startTime := time.Now().In(loc).String()
	return startTime[:len(startTime)-18]
}

func GetKind(kind string) string {
	if IsDev() {
		return kind + KindSuffix
	}
	return kind
}

func SetKind(val string) {
	if IsDev() {
		KindSuffix = GetTimeString()
		KindSuffix += val
		fmt.Printf("KindSuffix :%v\n", KindSuffix)
	}
}

func IsDev() bool {
	return appengine.IsDevAppServer()
}
