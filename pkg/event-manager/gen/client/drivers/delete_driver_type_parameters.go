///////////////////////////////////////////////////////////////////////
// Copyright (c) 2017 VMware, Inc. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
///////////////////////////////////////////////////////////////////////

// Code generated by go-swagger; DO NOT EDIT.

package drivers

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"net/http"
	"time"

	"golang.org/x/net/context"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	cr "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/swag"

	strfmt "github.com/go-openapi/strfmt"
)

// NewDeleteDriverTypeParams creates a new DeleteDriverTypeParams object
// with the default values initialized.
func NewDeleteDriverTypeParams() *DeleteDriverTypeParams {
	var ()
	return &DeleteDriverTypeParams{

		timeout: cr.DefaultTimeout,
	}
}

// NewDeleteDriverTypeParamsWithTimeout creates a new DeleteDriverTypeParams object
// with the default values initialized, and the ability to set a timeout on a request
func NewDeleteDriverTypeParamsWithTimeout(timeout time.Duration) *DeleteDriverTypeParams {
	var ()
	return &DeleteDriverTypeParams{

		timeout: timeout,
	}
}

// NewDeleteDriverTypeParamsWithContext creates a new DeleteDriverTypeParams object
// with the default values initialized, and the ability to set a context for a request
func NewDeleteDriverTypeParamsWithContext(ctx context.Context) *DeleteDriverTypeParams {
	var ()
	return &DeleteDriverTypeParams{

		Context: ctx,
	}
}

// NewDeleteDriverTypeParamsWithHTTPClient creates a new DeleteDriverTypeParams object
// with the default values initialized, and the ability to set a custom HTTPClient for a request
func NewDeleteDriverTypeParamsWithHTTPClient(client *http.Client) *DeleteDriverTypeParams {
	var ()
	return &DeleteDriverTypeParams{
		HTTPClient: client,
	}
}

/*DeleteDriverTypeParams contains all the parameters to send to the API endpoint
for the delete driver type operation typically these are written to a http.Request
*/
type DeleteDriverTypeParams struct {

	/*XDispatchOrg*/
	XDispatchOrg string
	/*DriverTypeName
	  Name of the driver type to work on

	*/
	DriverTypeName string
	/*Tags
	  Filter based on tags

	*/
	Tags []string

	timeout    time.Duration
	Context    context.Context
	HTTPClient *http.Client
}

// WithTimeout adds the timeout to the delete driver type params
func (o *DeleteDriverTypeParams) WithTimeout(timeout time.Duration) *DeleteDriverTypeParams {
	o.SetTimeout(timeout)
	return o
}

// SetTimeout adds the timeout to the delete driver type params
func (o *DeleteDriverTypeParams) SetTimeout(timeout time.Duration) {
	o.timeout = timeout
}

// WithContext adds the context to the delete driver type params
func (o *DeleteDriverTypeParams) WithContext(ctx context.Context) *DeleteDriverTypeParams {
	o.SetContext(ctx)
	return o
}

// SetContext adds the context to the delete driver type params
func (o *DeleteDriverTypeParams) SetContext(ctx context.Context) {
	o.Context = ctx
}

// WithHTTPClient adds the HTTPClient to the delete driver type params
func (o *DeleteDriverTypeParams) WithHTTPClient(client *http.Client) *DeleteDriverTypeParams {
	o.SetHTTPClient(client)
	return o
}

// SetHTTPClient adds the HTTPClient to the delete driver type params
func (o *DeleteDriverTypeParams) SetHTTPClient(client *http.Client) {
	o.HTTPClient = client
}

// WithXDispatchOrg adds the xDispatchOrg to the delete driver type params
func (o *DeleteDriverTypeParams) WithXDispatchOrg(xDispatchOrg string) *DeleteDriverTypeParams {
	o.SetXDispatchOrg(xDispatchOrg)
	return o
}

// SetXDispatchOrg adds the xDispatchOrg to the delete driver type params
func (o *DeleteDriverTypeParams) SetXDispatchOrg(xDispatchOrg string) {
	o.XDispatchOrg = xDispatchOrg
}

// WithDriverTypeName adds the driverTypeName to the delete driver type params
func (o *DeleteDriverTypeParams) WithDriverTypeName(driverTypeName string) *DeleteDriverTypeParams {
	o.SetDriverTypeName(driverTypeName)
	return o
}

// SetDriverTypeName adds the driverTypeName to the delete driver type params
func (o *DeleteDriverTypeParams) SetDriverTypeName(driverTypeName string) {
	o.DriverTypeName = driverTypeName
}

// WithTags adds the tags to the delete driver type params
func (o *DeleteDriverTypeParams) WithTags(tags []string) *DeleteDriverTypeParams {
	o.SetTags(tags)
	return o
}

// SetTags adds the tags to the delete driver type params
func (o *DeleteDriverTypeParams) SetTags(tags []string) {
	o.Tags = tags
}

// WriteToRequest writes these params to a swagger request
func (o *DeleteDriverTypeParams) WriteToRequest(r runtime.ClientRequest, reg strfmt.Registry) error {

	if err := r.SetTimeout(o.timeout); err != nil {
		return err
	}
	var res []error

	// header param X-Dispatch-Org
	if err := r.SetHeaderParam("X-Dispatch-Org", o.XDispatchOrg); err != nil {
		return err
	}

	// path param driverTypeName
	if err := r.SetPathParam("driverTypeName", o.DriverTypeName); err != nil {
		return err
	}

	valuesTags := o.Tags

	joinedTags := swag.JoinByFormat(valuesTags, "multi")
	// query array param tags
	if err := r.SetQueryParam("tags", joinedTags...); err != nil {
		return err
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}
