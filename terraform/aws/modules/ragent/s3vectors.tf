resource "aws_s3vectors_vector_bucket" "ragent" {
  count = local.is_s3_vectors ? 1 : 0

  vector_bucket_name = var.s3vectors_bucket_name

  tags = merge(local.common_tags, {
    Name = var.s3vectors_bucket_name
  })
}

resource "aws_s3vectors_index" "ragent" {
  count = local.is_s3_vectors ? 1 : 0

  index_name         = var.s3vectors_index_name
  vector_bucket_name = aws_s3vectors_vector_bucket.ragent[0].vector_bucket_name

  dimension       = var.s3vectors_dimension
  distance_metric = "cosine"
  data_type       = "float32"
}
