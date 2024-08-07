/*
 * Cce API
 *
 * CCE
 *
 * OpenAPI spec version: v1beta
 *
 * Generated by: https://github.com/swagger-api/swagger-codegen.git
 */

use std::borrow::Borrow;
use std::borrow::Cow;
use std::collections::HashMap;
use std::rc::Rc;

use futures;
use futures::{Future, Stream};
use hyper;
use serde_json;

use hyper::header::UserAgent;

use super::{configuration, Error};

pub struct EndpointApiClient<C: hyper::client::Connect> {
    configuration: Rc<configuration::Configuration<C>>,
}

impl<C: hyper::client::Connect> EndpointApiClient<C> {
    pub fn new(configuration: Rc<configuration::Configuration<C>>) -> EndpointApiClient<C> {
        EndpointApiClient {
            configuration: configuration,
        }
    }
}

pub trait EndpointApi {
    fn endpoint_extplugin_status_get(
        &self,
        owner: &str,
        container_id: &str,
    ) -> Box<Future<Item = ::models::ExtFeatureData, Error = Error<serde_json::Value>>>;
    fn endpoint_probe_put(
        &self,
        owner: &str,
        container_id: &str,
        netns: &str,
        ifname: &str,
        cni_driver: &str,
    ) -> Box<Future<Item = ::models::EndpointProbeResponse, Error = Error<serde_json::Value>>>;
}

impl<C: hyper::client::Connect> EndpointApi for EndpointApiClient<C> {
    fn endpoint_extplugin_status_get(
        &self,
        owner: &str,
        container_id: &str,
    ) -> Box<Future<Item = ::models::ExtFeatureData, Error = Error<serde_json::Value>>> {
        let configuration: &configuration::Configuration<C> = self.configuration.borrow();

        let method = hyper::Method::Get;

        let query_string = {
            let mut query = ::url::form_urlencoded::Serializer::new(String::new());
            query.append_pair("owner", &owner.to_string());
            query.append_pair("containerID", &container_id.to_string());
            query.finish()
        };
        let uri_str = format!(
            "{}/endpoint/extplugin/status?{}",
            configuration.base_path, query_string
        );

        // TODO(farcaller): handle error
        // if let Err(e) = uri {
        //     return Box::new(futures::future::err(e));
        // }
        let mut uri: hyper::Uri = uri_str.parse().unwrap();

        let mut req = hyper::Request::new(method, uri);

        if let Some(ref user_agent) = configuration.user_agent {
            req.headers_mut()
                .set(UserAgent::new(Cow::Owned(user_agent.clone())));
        }

        // send request
        Box::new(
            configuration
                .client
                .request(req)
                .map_err(|e| Error::from(e))
                .and_then(|resp| {
                    let status = resp.status();
                    resp.body()
                        .concat2()
                        .and_then(move |body| Ok((status, body)))
                        .map_err(|e| Error::from(e))
                })
                .and_then(|(status, body)| {
                    if status.is_success() {
                        Ok(body)
                    } else {
                        Err(Error::from((status, &*body)))
                    }
                })
                .and_then(|body| {
                    let parsed: Result<::models::ExtFeatureData, _> = serde_json::from_slice(&body);
                    parsed.map_err(|e| Error::from(e))
                }),
        )
    }

    fn endpoint_probe_put(
        &self,
        owner: &str,
        container_id: &str,
        netns: &str,
        ifname: &str,
        cni_driver: &str,
    ) -> Box<Future<Item = ::models::EndpointProbeResponse, Error = Error<serde_json::Value>>> {
        let configuration: &configuration::Configuration<C> = self.configuration.borrow();

        let method = hyper::Method::Put;

        let query_string = {
            let mut query = ::url::form_urlencoded::Serializer::new(String::new());
            query.append_pair("owner", &owner.to_string());
            query.append_pair("containerID", &container_id.to_string());
            query.append_pair("netns", &netns.to_string());
            query.append_pair("ifname", &ifname.to_string());
            query.append_pair("cni-driver", &cni_driver.to_string());
            query.finish()
        };
        let uri_str = format!(
            "{}/endpoint/probe?{}",
            configuration.base_path, query_string
        );

        // TODO(farcaller): handle error
        // if let Err(e) = uri {
        //     return Box::new(futures::future::err(e));
        // }
        let mut uri: hyper::Uri = uri_str.parse().unwrap();

        let mut req = hyper::Request::new(method, uri);

        if let Some(ref user_agent) = configuration.user_agent {
            req.headers_mut()
                .set(UserAgent::new(Cow::Owned(user_agent.clone())));
        }

        // send request
        Box::new(
            configuration
                .client
                .request(req)
                .map_err(|e| Error::from(e))
                .and_then(|resp| {
                    let status = resp.status();
                    resp.body()
                        .concat2()
                        .and_then(move |body| Ok((status, body)))
                        .map_err(|e| Error::from(e))
                })
                .and_then(|(status, body)| {
                    if status.is_success() {
                        Ok(body)
                    } else {
                        Err(Error::from((status, &*body)))
                    }
                })
                .and_then(|body| {
                    let parsed: Result<::models::EndpointProbeResponse, _> =
                        serde_json::from_slice(&body);
                    parsed.map_err(|e| Error::from(e))
                }),
        )
    }
}
