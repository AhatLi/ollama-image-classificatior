#!/usr/bin/env python
"""SigLIP 라벨 문장을 바꾼 뒤, 이미지를 다시 처리하지 않고 content/medium만 재계산한다.

각 폴더의 `.meta.jsonl`(메타)과 `.siglip.f16`(이미지 임베딩)을 읽어 새 라벨로 점수를 다시 내고,
값이 바뀐 파일만 갱신된 줄을 `.meta.jsonl` 끝에 덧붙인다 (같은 file은 마지막 줄이 유효).

사용: python tagger/rescore.py <루트> [<루트> ...] [--labels tagger/labels/siglip_labels.json] [--dry-run]
"""
import argparse
import json
import os
import sys

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from tagger_server import load_labels, softmax  # noqa: E402

DIM = 768
REC = DIM * 2


def rescore_dir(d, groups, scale, labels_hash, dry):
    meta_path, emb_path = os.path.join(d, ".meta.jsonl"), os.path.join(d, ".siglip.f16")
    last = {}
    with open(meta_path, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                r = json.loads(line)
            except ValueError:
                continue
            if "file" in r:
                last[r["file"]] = r
    recs = [r for r in last.values() if "err" not in r and isinstance(r.get("emb"), int) and r["emb"] >= 0
            and r.get("labels") != labels_hash]
    if not recs or not os.path.exists(emb_path):
        return len(last), 0
    n_emb = os.path.getsize(emb_path) // REC
    recs = [r for r in recs if r["emb"] < n_emb]
    if not recs:
        return len(last), 0
    mm = np.memmap(emb_path, dtype="<f2", mode="r", shape=(n_emb, DIM))
    V = np.asarray(mm[[r["emb"] for r in recs]], dtype=np.float32)
    V /= np.linalg.norm(V, axis=1, keepdims=True)

    cat = med = None
    if "category" in groups:
        names, mat = groups["category"]
        keep = [i for i, n in enumerate(names) if not n.startswith("nsfw_")]
        cat = ([names[i] for i in keep], scale * (V @ mat[keep].T))
    if "medium" in groups:
        names, mat = groups["medium"]
        med = (names, scale * (V @ mat.T))

    out = []
    for k, r in enumerate(recs):
        if cat:
            p = softmax(cat[1][k])
            r["content_top"] = [[cat[0][i], round(float(p[i]), 3)] for i in np.argsort(-p)[:3]]
        if med:
            p = softmax(med[1][k])
            i = int(np.argmax(p))
            r["medium"], r["medium_p"] = med[0][i], round(float(p[i]), 3)
        r["labels"] = labels_hash
        out.append(json.dumps(r, ensure_ascii=False, separators=(",", ":")) + "\n")
    if not dry:
        fd = os.open(meta_path, os.O_WRONLY | os.O_APPEND)
        try:
            for line in out:  # 줄 단위 write: 분류기가 동시에 덧붙여도 줄이 섞이지 않게
                os.write(fd, line.encode("utf-8"))
        finally:
            os.close(fd)
    return len(last), len(out)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("roots", nargs="+")
    ap.add_argument("--labels", default=os.path.join(os.path.dirname(os.path.abspath(__file__)), "labels", "siglip_labels.json"))
    ap.add_argument("--dry-run", action="store_true")
    a = ap.parse_args()

    groups, scale, labels_hash = load_labels(a.labels)
    print(f"labels={labels_hash}")
    total = changed = dirs = 0
    for root in a.roots:
        for d, subdirs, files in os.walk(root):
            subdirs[:] = sorted(s for s in subdirs if not s.startswith("."))
            if ".meta.jsonl" not in files:
                continue
            n, c = rescore_dir(d, groups, scale, labels_hash, a.dry_run)
            dirs += 1
            total += n
            changed += c
            if c:
                print(f"{d}: {c}/{n} 갱신")
    print(f"폴더 {dirs}개, 이미지 {total}장 중 {changed}장 갱신" + (" (dry-run, 기록 안 함)" if a.dry_run else ""))


if __name__ == "__main__":
    main()
