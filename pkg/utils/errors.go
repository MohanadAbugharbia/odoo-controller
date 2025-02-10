package utils

import "errors"

var ErrSecretInfoMissing = errors.New("secret information missing")

var ErrSecretNotFound = errors.New("secret not found")

var ErrSecretKeyNotFound = errors.New("key inside secret not found")

// Database related errors
var ErrFailedToGetDbHost = errors.New("failed to get database host")
var ErrFailedToGetDbPort = errors.New("failed to get database port")
var ErrFailedToGetDbUser = errors.New("failed to get database user")
var ErrFailedToGetDbPassword = errors.New("failed to get database password")
var ErrFailedToGetDbName = errors.New("failed to get database name")
var ErrFailedToGetDbSslMode = errors.New("failed to get database ssl mode")
var ErrFailedToGetDbMaxConns = errors.New("failed to get database max connections")
