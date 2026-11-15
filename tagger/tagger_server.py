#!/usr/bin/env python
"""ONNX 태거 서비스: SigLIP 2(vision) + NSFW 판별기 + WD 태거 v3.

분류기(Go)가 자식 프로세스로 띄운다. 판정은 하지 않고 원시 점수만 돌려준다.
의존성: onnxruntime, numpy, Pillow (그 외는 표준 라이브러리).

  GET  /health            -> {"status":"ok", ...}
  POST /classify          -> body {"path": "/abs/path/to/image"}

모델 디렉터리에 필요한 파일:
  siglip2_vision.onnx, nsfw_vit.onnx, wd_vit_v3.onnx, wd_vit_v3_tags.csv
라벨: <labels>.json (rows: group/label/prompt) + 같은 이름의 .npz (emb, logit_scale)
"""
import argparse
import base64
import csv
import hashlib
import json
import os
import sys
import threading
import time
from collections import OrderedDict
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import numpy as np
import onnxruntime as ort
from PIL import Image

MODELS_ID = "siglip2-b16-224+wd-vit-v3+nsfw-vit"
WD_GENERAL_TH = 0.35
WD_CHAR_TH = 0.85


class BadImage(Exception):
    pass


def softmax(x):
    e = np.exp(x - x.max())
    return e / e.sum()


def load_labels(json_path):
    """라벨 json/npz를 읽어 {그룹: (라벨 이름들, 정규화된 라벨 임베딩 행렬)}, scale, 해시를 돌려준다."""
    raw = open(json_path, "rb").read()
    meta = json.loads(raw.decode("utf-8"))
    z = np.load(os.path.splitext(json_path)[0] + ".npz")
    emb = z["emb"].astype(np.float32)
    scale = float(z["logit_scale"])
    groups = OrderedDict()
    for i, r in enumerate(meta["rows"]):
        groups.setdefault(r["group"], OrderedDict()).setdefault(r["label"], []).append(i)
    out = {}
    for g, labels in groups.items():  # 라벨당 문장 임베딩 평균 후 재정규화
        names = list(labels)
        mat = np.stack([emb[labels[n]].mean(0) for n in names])
        out[g] = (names, mat / np.linalg.norm(mat, axis=1, keepdims=True))
    return out, scale, hashlib.sha1(raw).hexdigest()[:10]


def score_labels(groups, scale, v):
    """정규화된 이미지 임베딩 v -> (content_top, medium, medium_p). rescore.py와 공유하는 계산."""
    content_top, medium, medium_p = [], "", 0.0
    if "category" in groups:
        names, mat = groups["category"]
        keep = [i for i, n in enumerate(names) if not n.startswith("nsfw_")]  # 수위는 전용 판별기 담당
        p = softmax(scale * (mat[keep] @ v))
        content_top = [[names[keep[i]], round(float(p[i]), 3)] for i in np.argsort(-p)[:3]]
    if "medium" in groups:
        names, mat = groups["medium"]
        p = softmax(scale * (mat @ v))
        i = int(np.argmax(p))
        medium, medium_p = names[i], round(float(p[i]), 3)
    return content_top, medium, medium_p


class Tagger:
    def __init__(self, model_dir, labels_json, threads):
        so = ort.SessionOptions()
        so.intra_op_num_threads = threads
        prov = ["CPUExecutionProvider"]
        self.siglip = ort.InferenceSession(os.path.join(model_dir, "siglip2_vision.onnx"), so, providers=prov)
        self.nsfw = ort.InferenceSession(os.path.join(model_dir, "nsfw_vit.onnx"), so, providers=prov)
        self.wd = ort.InferenceSession(os.path.join(model_dir, "wd_vit_v3.onnx"), so, providers=prov)

        self.siglip_in = self.siglip.get_inputs()[0].name
        outs = [o.name for o in self.siglip.get_outputs()]
        self.siglip_out = "pooler_output" if "pooler_output" in outs else outs[-1]
        self.nsfw_in = self.nsfw.get_inputs()[0].name
        wd_in = self.wd.get_inputs()[0]
        self.wd_in, self.wd_size = wd_in.name, int(wd_in.shape[1])

        names, cats = [], []
        with open(os.path.join(model_dir, "wd_vit_v3_tags.csv"), newline="", encoding="utf-8") as f:
            for row in csv.DictReader(f):
                names.append(row["name"])
                cats.append(int(row["category"]))  # 0 general, 4 character, 9 rating
        self.wd_names = names
        cats = np.array(cats)
        self.wd_rating = np.where(cats == 9)[0]
        self.wd_general = cats == 0
        self.wd_char = cats == 4

        self.groups, self.scale, self.labels_hash = load_labels(labels_json)
        self.lock = threading.Lock()  # 추론은 한 번에 하나만 (CPU/메모리 보호)

    @staticmethod
    def _load(path):
        try:
            im = Image.open(path)
            animated = getattr(im, "n_frames", 1) > 1
            im.seek(0)
            w, h = im.size
            im = im.convert("RGBA")
            bg = Image.new("RGBA", im.size, (255, 255, 255, 255))
            bg.alpha_composite(im)
            return bg.convert("RGB"), w, h, animated
        except FileNotFoundError:
            raise
        except Exception as e:  # 깨진 파일, 지원 안 하는 형식 등
            raise BadImage(f"{type(e).__name__}: {e}")

    def classify(self, path):
        t0 = time.time()
        im, w, h, animated = self._load(path)
        t1 = time.time()
        with self.lock:
            # SigLIP: 224 squash, bilinear, mean/std 0.5, NCHW
            x = (np.asarray(im.resize((224, 224), Image.BILINEAR), dtype=np.float32) / 255.0 - 0.5) / 0.5
            v = self.siglip.run([self.siglip_out], {self.siglip_in: np.ascontiguousarray(x.transpose(2, 0, 1))[None]})[0][0]
            v = v / np.linalg.norm(v)
            content_top, medium, medium_p = score_labels(self.groups, self.scale, v)
            t2 = time.time()

            # NSFW: 384 squash, bilinear, mean/std 0.5, NCHW. 출력 0=sfw, 1=nsfw
            x = (np.asarray(im.resize((384, 384), Image.BILINEAR), dtype=np.float32) / 255.0 - 0.5) / 0.5
            p = softmax(self.nsfw.run(None, {self.nsfw_in: np.ascontiguousarray(x.transpose(2, 0, 1))[None]})[0][0])
            nsfw = round(float(p[1]), 3)
            t3 = time.time()

            # WD v3: 흰색 정사각 패딩, bicubic, BGR, 0~255, NHWC
            side = max(im.size)
            sq = Image.new("RGB", (side, side), (255, 255, 255))
            sq.paste(im, ((side - im.width) // 2, (side - im.height) // 2))
            sq = sq.resize((self.wd_size, self.wd_size), Image.BICUBIC)
            x = np.ascontiguousarray(np.asarray(sq, dtype=np.float32)[:, :, ::-1])[None]
            p = self.wd.run(None, {self.wd_in: x})[0][0]
            t4 = time.time()

        rating = self.wd_names[self.wd_rating[int(np.argmax(p[self.wd_rating]))]]
        tags = sorted(([self.wd_names[i], round(float(p[i]), 3)] for i in np.where(self.wd_general & (p >= WD_GENERAL_TH))[0]),
                      key=lambda t: -t[1])
        chars = sorted(([self.wd_names[i], round(float(p[i]), 3)] for i in np.where(self.wd_char & (p >= WD_CHAR_TH))[0]),
                       key=lambda t: -t[1])
        return {
            "ok": True,
            "content_top": content_top, "medium": medium, "medium_p": medium_p,
            "nsfw": nsfw, "rating": rating, "tags": tags, "chars": chars,
            "w": w, "h": h, "animated": bool(animated),
            "emb": base64.b64encode(v.astype("<f2").tobytes()).decode("ascii"),
            "labels": self.labels_hash, "models": MODELS_ID,
            "ms": {"load": int((t1 - t0) * 1000), "siglip": int((t2 - t1) * 1000),
                   "nsfw": int((t3 - t2) * 1000), "wd": int((t4 - t3) * 1000)},
        }


def make_handler(tagger):
    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def _send(self, code, obj):
            body = json.dumps(obj, ensure_ascii=False).encode("utf-8")
            self.send_response(code)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            if self.path == "/health":
                self._send(200, {"status": "ok", "models": MODELS_ID, "labels": tagger.labels_hash})
            else:
                self._send(404, {"ok": False, "error": "not found"})

        def do_POST(self):
            if self.path != "/classify":
                self._send(404, {"ok": False, "error": "not found"})
                return
            try:
                n = int(self.headers.get("Content-Length") or 0)
                path = json.loads(self.rfile.read(n).decode("utf-8"))["path"]
                if not isinstance(path, str) or not path:
                    raise ValueError("empty path")
            except Exception as e:
                self._send(400, {"ok": False, "error": f"bad request: {e}"})
                return
            try:
                self._send(200, tagger.classify(path))
            except FileNotFoundError:
                self._send(404, {"ok": False, "error": "file not found"})
            except BadImage as e:
                self._send(422, {"ok": False, "error": str(e)})
            except Exception as e:  # 모델 쪽 오류: 이미지 탓이 아닐 수 있으므로 5xx
                self._send(500, {"ok": False, "error": f"{type(e).__name__}: {e}"})

        def log_message(self, fmt, *args):  # 요청마다 로그를 남기지 않는다
            pass

    return Handler


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=8090)
    ap.add_argument("--threads", type=int, default=4)
    ap.add_argument("--models", default="models/tagger", help="모델 디렉터리")
    ap.add_argument("--labels", default="tagger/labels/siglip_labels.json", help="라벨 json (같은 이름의 .npz 필요)")
    a = ap.parse_args()

    try:
        os.nice(19)  # 네트워크 데몬보다 낮은 우선순위
    except Exception:
        pass

    t0 = time.time()
    tagger = Tagger(a.models, a.labels, a.threads)
    print(f"[tagger] models loaded in {time.time() - t0:.1f}s, labels={tagger.labels_hash}, "
          f"threads={a.threads}, listening on {a.host}:{a.port}", flush=True)
    srv = ThreadingHTTPServer((a.host, a.port), make_handler(tagger))
    srv.daemon_threads = True
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    sys.exit(main())
