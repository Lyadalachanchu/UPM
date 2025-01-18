from flask import Flask, request, jsonify
import torch
from transformers import AutoTokenizer, AutoModel

app = Flask(__name__)

# Load the model and tokenizer
model_name = "allenai/specter2_base"
tokenizer = AutoTokenizer.from_pretrained(model_name)
model = AutoModel.from_pretrained(model_name)
model.eval()

# Set the device
device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
print(f"Device: {device}")
model = model.to(device)

@app.route("/calculate_embeddings", methods=["POST"])
def calculate_embeddings():
    """
    Accepts a list of titles and abstracts, calculates embeddings, and returns them.
    """
    data = request.json
    texts = data.get("texts", [])

    if not texts:
        return jsonify({"error": "No texts provided"}), 400

    # Tokenize and calculate embeddings
    inputs = tokenizer(
        texts,
        padding=True,
        truncation=True,
        return_tensors="pt",
        max_length=512,
    )

    inputs = {key: value.to(device) for key, value in inputs.items()}

    with torch.no_grad():
        outputs = model(**inputs)
        embeddings = outputs.last_hidden_state[:, 0, :].cpu().tolist()

    print("Embeddings calculated successfully: ", embeddings)

    return jsonify({"embeddings": embeddings})

if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000)
