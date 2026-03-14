provider "openfga" {
  api_url = var.openfga_api_url
}

resource "openfga_store" "this" {
  name = var.store_name
}

data "openfga_authorization_model_document" "this" {
  file = "${path.module}/model.fga"
}

resource "openfga_authorization_model" "this" {
  store_id = openfga_store.this.id
  model    = data.openfga_authorization_model_document.this.json
}
