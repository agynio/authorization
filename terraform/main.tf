provider "openfga" {
  api_url = var.openfga_api_url
}

resource "openfga_store" "this" {
  name = var.store_name
}

data "openfga_authorization_model_document" "this" {
  # Single source of truth: the model lives in internal/authz/model.fga so the
  # service image can go:embed it. Git module sources fetch the whole repo, so
  # this relative path resolves both locally and when consumed remotely.
  dsl = file("${path.module}/../internal/authz/model.fga")
}

resource "openfga_authorization_model" "this" {
  store_id   = openfga_store.this.id
  model_json = data.openfga_authorization_model_document.this.result
}
