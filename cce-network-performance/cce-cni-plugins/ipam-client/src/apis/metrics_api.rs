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

pub struct MetricsApiClient<C: hyper::client::Connect> {
    configuration: Rc<configuration::Configuration<C>>,
}

impl<C: hyper::client::Connect> MetricsApiClient<C> {
    pub fn new(configuration: Rc<configuration::Configuration<C>>) -> MetricsApiClient<C> {
        MetricsApiClient {
            configuration: configuration,
        }
    }
}

pub trait MetricsApi {
    fn metrics_get(
        &self,
    ) -> Box<Future<Item = Vec<::models::Metric>, Error = Error<serde_json::Value>>>;
}

impl<C: hyper::client::Connect> MetricsApi for MetricsApiClient<C> {
    fn metrics_get(
        &self,
    ) -> Box<Future<Item = Vec<::models::Metric>, Error = Error<serde_json::Value>>> {
        let configuration: &configuration::Configuration<C> = self.configuration.borrow();

        let method = hyper::Method::Get;

        let query_string = {
            let mut query = ::url::form_urlencoded::Serializer::new(String::new());
            query.finish()
        };
        let uri_str = format!("{}/metrics/?{}", configuration.base_path, query_string);

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
                    let parsed: Result<Vec<::models::Metric>, _> = serde_json::from_slice(&body);
                    parsed.map_err(|e| Error::from(e))
                }),
        )
    }
}
