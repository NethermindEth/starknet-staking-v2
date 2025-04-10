package main

import "errors"

func entrypointInternalError(entrypointName string, err error) error {
	return errors.New("Error when calling entrypoint `" + entrypointName + "`: " + err.Error())
}

func entrypointResponseError(entrypointName string) error {
	return errors.New("Invalid response from entrypoint `" + entrypointName + "`")
}
