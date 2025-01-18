from tqdm import tqdm
import torch

def get_embeddings(reading_list, model, tokenizer):
    model.eval()
    embeddings = []
    BATCH_SIZE = 10 #TODO: Maybe we can go higher?
    for start in range(0, len(reading_list), BATCH_SIZE):
        text_batch = [
                d['title'] + tokenizer.sep_token + (d.get('abstract') or '')
                for d in reading_list[start:start + BATCH_SIZE]
            ]

        # Tokenize the input
        inputs = tokenizer(
            text_batch,
            padding=True,
            truncation=True,
            return_tensors="pt",
            return_token_type_ids=False,
            max_length=512
        )

        inputs = {key: value.to(model.device) for key, value in inputs.items()}

        # Forward pass through the model
        output = model(**inputs)

        # Extract the embeddings for the first token (e.g., [CLS] token)
        batch_embeddings = output.last_hidden_state[:, 0, :].cpu().detach()

        embeddings.append(batch_embeddings)

        # Clear CUDA cache if using GPU to free up memory
        if torch.cuda.is_available():
            torch.cuda.empty_cache()
    if len(embeddings) != 0:
        embeddings = torch.cat(embeddings, dim=0)
    else:
        embeddings = torch.zeros((1, 768))
    return embeddings

