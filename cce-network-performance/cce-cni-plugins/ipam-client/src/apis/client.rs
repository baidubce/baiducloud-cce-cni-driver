use std::rc::Rc;

use super::configuration::Configuration;
use hyper;

pub struct APIClient<C: hyper::client::Connect> {
    configuration: Rc<Configuration<C>>,
    daemon_api: Box<::apis::DaemonApi>,
    endpoint_api: Box<::apis::EndpointApi>,
    eni_api: Box<::apis::EniApi>,
    ipam_api: Box<::apis::IpamApi>,
    metrics_api: Box<::apis::MetricsApi>,
}

impl<C: hyper::client::Connect> APIClient<C> {
    pub fn new(configuration: Configuration<C>) -> APIClient<C> {
        let rc = Rc::new(configuration);

        APIClient {
            configuration: rc.clone(),
            daemon_api: Box::new(::apis::DaemonApiClient::new(rc.clone())),
            endpoint_api: Box::new(::apis::EndpointApiClient::new(rc.clone())),
            eni_api: Box::new(::apis::EniApiClient::new(rc.clone())),
            ipam_api: Box::new(::apis::IpamApiClient::new(rc.clone())),
            metrics_api: Box::new(::apis::MetricsApiClient::new(rc.clone())),
        }
    }

    pub fn daemon_api(&self) -> &::apis::DaemonApi {
        self.daemon_api.as_ref()
    }

    pub fn endpoint_api(&self) -> &::apis::EndpointApi {
        self.endpoint_api.as_ref()
    }

    pub fn eni_api(&self) -> &::apis::EniApi {
        self.eni_api.as_ref()
    }

    pub fn ipam_api(&self) -> &::apis::IpamApi {
        self.ipam_api.as_ref()
    }

    pub fn metrics_api(&self) -> &::apis::MetricsApi {
        self.metrics_api.as_ref()
    }
}
