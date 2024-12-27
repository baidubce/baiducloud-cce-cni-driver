/*
 * Cce API
 *
 * CCE
 *
 * OpenAPI spec version: v1beta
 *
 * Generated by: https://github.com/swagger-api/swagger-codegen.git
 */

/// ControllerStatus : Status of a controller  +k8s:deepcopy-gen=true

#[allow(unused_imports)]
use serde_json::Value;

#[derive(Debug, Serialize, Deserialize)]
pub struct ControllerStatus {
    /// Name of controller
    #[serde(rename = "name")]
    name: Option<String>,
    /// UUID of controller
    #[serde(rename = "uuid")]
    uuid: Option<String>,
    #[serde(rename = "configuration")]
    configuration: Option<::models::ControllerStatusConfiguration>,
    #[serde(rename = "status")]
    status: Option<::models::ControllerStatusStatus>,
}

impl ControllerStatus {
    /// Status of a controller  +k8s:deepcopy-gen=true
    pub fn new() -> ControllerStatus {
        ControllerStatus {
            name: None,
            uuid: None,
            configuration: None,
            status: None,
        }
    }

    pub fn set_name(&mut self, name: String) {
        self.name = Some(name);
    }

    pub fn with_name(mut self, name: String) -> ControllerStatus {
        self.name = Some(name);
        self
    }

    pub fn name(&self) -> Option<&String> {
        self.name.as_ref()
    }

    pub fn reset_name(&mut self) {
        self.name = None;
    }

    pub fn set_uuid(&mut self, uuid: String) {
        self.uuid = Some(uuid);
    }

    pub fn with_uuid(mut self, uuid: String) -> ControllerStatus {
        self.uuid = Some(uuid);
        self
    }

    pub fn uuid(&self) -> Option<&String> {
        self.uuid.as_ref()
    }

    pub fn reset_uuid(&mut self) {
        self.uuid = None;
    }

    pub fn set_configuration(&mut self, configuration: ::models::ControllerStatusConfiguration) {
        self.configuration = Some(configuration);
    }

    pub fn with_configuration(
        mut self,
        configuration: ::models::ControllerStatusConfiguration,
    ) -> ControllerStatus {
        self.configuration = Some(configuration);
        self
    }

    pub fn configuration(&self) -> Option<&::models::ControllerStatusConfiguration> {
        self.configuration.as_ref()
    }

    pub fn reset_configuration(&mut self) {
        self.configuration = None;
    }

    pub fn set_status(&mut self, status: ::models::ControllerStatusStatus) {
        self.status = Some(status);
    }

    pub fn with_status(mut self, status: ::models::ControllerStatusStatus) -> ControllerStatus {
        self.status = Some(status);
        self
    }

    pub fn status(&self) -> Option<&::models::ControllerStatusStatus> {
        self.status.as_ref()
    }

    pub fn reset_status(&mut self) {
        self.status = None;
    }
}