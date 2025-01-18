import torch
from adapters import AutoAdapterModel
from transformers import AutoTokenizer

from utils.arxiv_paper_retrieval import fetch_arxiv_papers_with_dates, fetch_all_papers_in_batches
from utils.embedding_utils import get_embeddings

if __name__ == "__main__":
    # Given a reading list, return the closest author
    # my_reading_list = ["arxivId1", "arxivId2"]
    # initial_paper_arxiv_ids = ["2308.06512"]
    initial_paper_arxiv_ids = fetch_arxiv_papers_with_dates(
        category="cs.IR",
        batch_size=0,
        max_papers=0,
        start_date="2020-01-01",
        end_date="2024-12-31"
    )
    print(initial_paper_arxiv_ids)
    papers = fetch_all_papers_in_batches(initial_paper_arxiv_ids)
    # print(papers)
    print(papers['2001.11402']['title'])
    print(papers['2001.11402']['references'][0])
    papers = {key: paper for key, paper in papers.items()
              if paper and paper.get('title') != None and paper.get('abstract') != None and len(paper.get('authors')) != 0}
    for k, p in papers.items():
        new_refs = []
        for r in p['references']:
            if r['title'] and r['abstract'] or len(r['authors']) != 0:
                new_refs.append(r)
        p['references'] = new_refs

    # load shit
    tokenizer = AutoTokenizer.from_pretrained('allenai/specter2_base')
    model = AutoAdapterModel.from_pretrained('allenai/specter2_base')
    model.load_adapter("allenai/specter2", source="hf", load_as="specter2", set_active=True)

    # do shit
    device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
    print(f"device: {device}")
    model = model.to(device)

    # Test get_embeddings function on the first paper
    a = get_embeddings(papers['2001.11402']['references'], model, tokenizer)
    print(a)
