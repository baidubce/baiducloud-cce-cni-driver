/*
 * Cce API
 *
 * CCE
 *
 * OpenAPI spec version: v1beta
 *
 * Generated by: https://github.com/swagger-api/swagger-codegen.git
 */

/// Metric : Metric information

#[allow(unused_imports)]
use serde_json::Value;

#[derive(Debug, Serialize, Deserialize)]
pub struct Metric {
    /// Name of the metric
    #[serde(rename = "name")]
    name: Option<String>,
    /// Value of the metric
    #[serde(rename = "value")]
    value: Option<f32>,
    /// Labels of the metric
    #[serde(rename = "labels")]
    labels: Option<::std::collections::HashMap<String, String>>,
}

impl Metric {
    /// Metric information
    pub fn new() -> Metric {
        Metric {
            name: None,
            value: None,
            labels: None,
        }
    }

    pub fn set_name(&mut self, name: String) {
        self.name = Some(name);
    }

    pub fn with_name(mut self, name: String) -> Metric {
        self.name = Some(name);
        self
    }

    pub fn name(&self) -> Option<&String> {
        self.name.as_ref()
    }

    pub fn reset_name(&mut self) {
        self.name = None;
    }

    pub fn set_value(&mut self, value: f32) {
        self.value = Some(value);
    }

    pub fn with_value(mut self, value: f32) -> Metric {
        self.value = Some(value);
        self
    }

    pub fn value(&self) -> Option<&f32> {
        self.value.as_ref()
    }

    pub fn reset_value(&mut self) {
        self.value = None;
    }

    pub fn set_labels(&mut self, labels: ::std::collections::HashMap<String, String>) {
        self.labels = Some(labels);
    }

    pub fn with_labels(mut self, labels: ::std::collections::HashMap<String, String>) -> Metric {
        self.labels = Some(labels);
        self
    }

    pub fn labels(&self) -> Option<&::std::collections::HashMap<String, String>> {
        self.labels.as_ref()
    }

    pub fn reset_labels(&mut self) {
        self.labels = None;
    }
}
