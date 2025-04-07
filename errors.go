package main

import "errors"

func entrypointInternalError(entrypointName string, err error) error {
	return errors.New("Error when calling entrypoint `" + entrypointName + "`: " + err.Error())
}

func entrypointResponseError(entrypointName string) error {
	return errors.New("Invalid response from entrypoint `" + entrypointName + "`")
}

func missingConfigGeneralField(missingFieldName string) error {
	return errors.New("you must specify the " + missingFieldName + " field in the config.json")
}

func missingConfigSignerField(missingFieldName string, signerFlag string) error {
	return errors.New("you must specify the " + missingFieldName + " field in the config.json when using " + signerFlag + " flag")
}
