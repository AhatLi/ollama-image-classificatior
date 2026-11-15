#!/bin/sh
# tagger 백엔드용 ONNX 모델 내려받기 (약 1.1GB). 저장소 루트에서: sh tagger/download_models.sh
set -e
M="${1:-models/tagger}"
H="https://huggingface.co"
mkdir -p "$M"
get() { [ -s "$M/$1" ] || curl -L --fail -sS -o "$M/$1" "$2"; ls -la "$M/$1"; }

get siglip2_vision.onnx  "$H/onnx-community/siglip2-base-patch16-224-ONNX/resolve/main/onnx/vision_model.onnx"
get nsfw_vit.onnx        "$H/AdamCodd/vit-base-nsfw-detector/resolve/main/onnx/model.onnx"
get wd_vit_v3.onnx       "$H/SmilingWolf/wd-vit-tagger-v3/resolve/main/model.onnx"
get wd_vit_v3_tags.csv   "$H/SmilingWolf/wd-vit-tagger-v3/resolve/main/selected_tags.csv"
echo "== 완료: $M =="
