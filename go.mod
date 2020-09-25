module github.com/MSpaceDev/CelbuxHelpers

replace github.com/MSpaceDev/CelbuxHelpers => ./

go 1.15

require (
	cloud.google.com/go v0.65.0
	cloud.google.com/go/datastore v1.2.0
	cloud.google.com/go/logging v1.1.0
	cloud.google.com/go/storage v1.11.0
	github.com/golang/gddo v0.0.0-20200831202555-721e228c7686
	golang.org/x/net v0.0.0-20200904194848-62affa334b73
	google.golang.org/genproto v0.0.0-20200911024640-645f7a48b24f
	google.golang.org/grpc v1.32.0
)
