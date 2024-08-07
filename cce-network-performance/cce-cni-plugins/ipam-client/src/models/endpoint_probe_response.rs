/*
 * Cce API
 *
 * CCE
 *
 * OpenAPI spec version: v1beta
 *
 * Generated by: https://github.com/swagger-api/swagger-codegen.git
 */

/// EndpointProbeResponse : EndpointProbeResponse is the response of probe endpoint

#[allow(unused_imports)]
use serde_json::Value;

#[derive(Debug, Serialize, Deserialize)]
pub struct EndpointProbeResponse {
    #[serde(rename = "bandWidth")]
    band_width: Option<::models::BandwidthOption>,
    #[serde(rename = "featureGates")]
    feature_gates: Option<Vec<::models::ExtFeatureData>>,
}

impl EndpointProbeResponse {
    /// EndpointProbeResponse is the response of probe endpoint
    pub fn new() -> EndpointProbeResponse {
        EndpointProbeResponse {
            band_width: None,
            feature_gates: None,
        }
    }

    pub fn set_band_width(&mut self, band_width: ::models::BandwidthOption) {
        self.band_width = Some(band_width);
    }

    pub fn with_band_width(
        mut self,
        band_width: ::models::BandwidthOption,
    ) -> EndpointProbeResponse {
        self.band_width = Some(band_width);
        self
    }

    pub fn band_width(&self) -> Option<&::models::BandwidthOption> {
        self.band_width.as_ref()
    }

    pub fn reset_band_width(&mut self) {
        self.band_width = None;
    }

    pub fn set_feature_gates(&mut self, feature_gates: Vec<::models::ExtFeatureData>) {
        self.feature_gates = Some(feature_gates);
    }

    pub fn with_feature_gates(
        mut self,
        feature_gates: Vec<::models::ExtFeatureData>,
    ) -> EndpointProbeResponse {
        self.feature_gates = Some(feature_gates);
        self
    }

    pub fn feature_gates(&self) -> Option<&Vec<::models::ExtFeatureData>> {
        self.feature_gates.as_ref()
    }

    pub fn reset_feature_gates(&mut self) {
        self.feature_gates = None;
    }
}
