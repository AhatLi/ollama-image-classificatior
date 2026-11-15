"""SigLIP 2 zero-shot용 라벨 문장 임베딩을 만든다 (PC에서 실행, 결과만 폰으로 올림).

출력: siglip_labels.npz  (emb[N,D] 정규화된 문장 임베딩, logit_scale, logit_bias)
      siglip_labels.json (그룹/라벨/문장 ↔ emb 행 번호)
라벨을 바꾸려면 GROUPS만 고치고 다시 실행하면 된다. 폰 쪽은 이미지 인코더만 돌린다.
문장을 바꾼 뒤에는 tagger/rescore.py 로 저장된 이미지 임베딩에서 점수만 다시 계산할 수 있다.

필요 패키지: torch, transformers, sentencepiece, protobuf, numpy
"""
import json
import numpy as np

MODEL = "google/siglip2-base-patch16-224"

GROUPS = {
    # 내용 카테고리. 라벨당 문장 여러 개의 임베딩을 평균(앙상블)한다. 수위(nsfw)는 여기서 다루지 않는다.
    "category": {
        "animal": [
            "a photo of an animal",
            "a photo of a dog",
            "a photo of a cat",
            "a photo of a bird, fish or insect",
            "a photo of a wild animal in nature",
            "a photo of farm animals such as sheep, cows or horses",
        ],
        "cosplay": [
            "a photo of a cosplayer wearing an anime character costume and wig",
            "a cosplay photo taken at a convention or studio",
        ],
        "document": [
            "a screenshot of text",
            "a screenshot of a chat or messenger conversation",
            "a screenshot of a web page, news article or social media post",
            "a scanned document or a page of a book",
            "a table, chart or infographic",
        ],
        "figure": [
            "a photo of an anime figure",
            "a photo of a plastic figurine or scale model on a desk or shelf",
            "a photo of a toy or doll",
        ],
        "food": [
            "a photo of food",
            "a photo of a meal on a plate",
            "a photo of a drink or dessert",
        ],
        "game_screen": [
            "a screenshot of a video game",
            "a video game screen with HUD and menus",
            "a mobile game screenshot",
        ],
        "grotesque": [
            "a photo of blood and gore",
            "a photo of a wound or injury",
            "a photo of a dead body",
            "a medical photo of a skin disease",
        ],
        "illustration": [
            "an anime illustration",
            "a digital painting of an anime girl",
            "anime fan art of a character",
            "a colored anime-style drawing",
            "a screenshot from an anime",
        ],
        "landscape": [
            "a landscape photo of nature",
            "a photo of a city street or buildings",
            "a photo of the sky, sea or mountains",
        ],
        "manga": [
            "a manga page with panels and speech bubbles",
            "a black and white comic page",
            "a webtoon or comic strip with speech bubbles",
            "a four-panel comic",
        ],
        "meme": [
            "an image macro meme with large caption text at the top and bottom",
            "a funny edited picture with text overlaid on it",
            "a collage of pictures with joke captions",
        ],
        "object": [
            "a photo of an object",
            "a product photo",
            "a photo of an electronic device",
            "a photo of a car or vehicle",
            "a photo of clothes, shoes or accessories without a person",
        ],
        "real_person": [
            "a photo of a person in everyday clothes",
            "a portrait photo of a woman",
            "a portrait photo of a man",
            "a photo of a group of people",
            "a selfie",
            "a photo of a celebrity or idol",
        ],
    },
    # 매체: 실사/2D 구분
    "medium": {
        "photo": ["a photograph", "a real-life photo taken with a camera"],
        "anime_2d": ["an anime-style drawing", "a 2d illustration or painting"],
        "manga_page": ["a manga or comic page with panels"],
        "3d_render": ["a 3d rendered image", "a cgi render"],
        "screenshot": ["a screenshot of a phone or computer screen",
                       "a screenshot with user interface elements and text"],
    },
}

_cache = {}


def _load():
    if "m" not in _cache:
        from transformers import AutoModel, AutoTokenizer
        _cache["t"] = AutoTokenizer.from_pretrained(MODEL)
        _cache["m"] = AutoModel.from_pretrained(MODEL).eval()
    return _cache["t"], _cache["m"]


def encode(groups):
    """groups -> (emb[N,768] 정규화, rows[{group,label,prompt}], logit_scale, logit_bias)"""
    import torch
    tok, model = _load()
    rows, texts = [], []
    for g, labels in groups.items():
        for label, prompts in labels.items():
            for p in prompts:
                rows.append({"group": g, "label": label, "prompt": p})
                texts.append(p.lower())
    inputs = tok(texts, padding="max_length", max_length=64, truncation=True, return_tensors="pt")
    with torch.no_grad():
        emb = model.get_text_features(input_ids=inputs["input_ids"])
    emb = getattr(emb, "pooler_output", emb)  # transformers 버전에 따라 텐서 또는 출력 객체
    emb = torch.nn.functional.normalize(emb, dim=-1).numpy().astype(np.float32)
    return emb, rows, float(model.logit_scale.detach().exp()), float(model.logit_bias.detach())


def main():
    emb, rows, scale, bias = encode(GROUPS)
    np.savez("siglip_labels.npz", emb=emb, logit_scale=scale, logit_bias=bias)
    with open("siglip_labels.json", "w", encoding="utf-8") as f:
        json.dump({"model": MODEL, "logit_scale": scale, "logit_bias": bias, "rows": rows}, f,
                  ensure_ascii=False, indent=1)
    print("rows", len(rows), "dim", emb.shape[1], "scale", round(scale, 2), "bias", round(bias, 2))


if __name__ == "__main__":
    main()
