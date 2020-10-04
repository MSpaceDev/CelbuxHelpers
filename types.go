package celbuxhelpers

import (
	"cloud.google.com/go/datastore"
	"cloud.google.com/go/errorreporting"
	"cloud.google.com/go/storage"
)

var KindSuffix = GetTimeString()

/* GCP Helpers
 * Celbux helpers for easy & neat integration with GCP in main app engine code
 * Requirement: Call initialiseClients(projectID) in main app start up
 */
var ErrorClient *errorreporting.Client
var DatastoreClient *datastore.Client
var StorageClient *storage.Client
var LoggingClient *logging.Client
var TasksClient *cloudtasks.Client