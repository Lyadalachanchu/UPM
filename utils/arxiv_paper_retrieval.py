import os
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
import requests
from requests.adapters import HTTPAdapter, Retry
import json

# given an arxivID, return the title, abstract, authors, and 10 references
def get_paper_info(arxiv_id, session):
    """
    Fetches the paper data (including citations/references) by arXiv ID from Semantic Scholar.
    Returns a dict with relevant fields or None on error.
    """
    base_url = "https://api.semanticscholar.org/graph/v1/paper/ARXIV:"
    # Include 'abstract' in fields so we can store it in the cache
    fields = "title,abstract,authors,references.title,references.abstract,references.authors"
    url = f"{base_url}{arxiv_id}?fields={fields}"

    try:
        response = session.get(url, timeout=10)
        if response.status_code == 200:
            return response.json()
        else:
            print(f"Error for {arxiv_id}: Status code {response.status_code}")
            return None
    except requests.exceptions.RequestException as e:
        print(f"Network error for {arxiv_id}: {e}")
        return None


# parallel stuff:
CACHE_FILE = "citations_cache.json"


def get_session():
    """
    Creates a requests Session with retry logic.
    Retries up to 5 times on HTTP 429/5xx errors, with exponential backoff:
      - 1s, 2s, 4s, 8s, 16s
    """
    session = requests.Session()

    # Exponential backoff is governed by 'backoff_factor=1'.
    # With each retry, the backoff time is:
    #     backoff_factor * (2^(retry_count - 1))
    # So for retries 1..4, the wait times are 1, 2, 4, 8 (seconds), etc.
    retry_strategy = Retry(
        total=10,  # Total retries
        backoff_factor=1,  # Exponential backoff factor
        status_forcelist=[429, 500, 502, 503, 504],  # Retry on these HTTP status codes
        allowed_methods=["GET"],  # Only retry on GET requests
    )
    adapter = HTTPAdapter(max_retries=retry_strategy)

    # Mount the adapter to handle both HTTP and HTTPS
    session.mount("https://", adapter)
    session.mount("http://", adapter)

    return session


CACHE_FILE = "citations_cache.json"

def load_cache():
    """
    Load existing results from a local JSON file (if it exists).
    Returns a dict of {arxiv_id: data}.
    """
    if not os.path.exists(CACHE_FILE):
        return {}
    with open(CACHE_FILE, "r", encoding="utf-8") as f:
        return json.load(f)

def save_cache(cache_data):
    """
    Saves results to a local JSON file.
    """
    with open(CACHE_FILE, "w", encoding="utf-8") as f:
        json.dump(cache_data, f, ensure_ascii=False, indent=2)

def process_paper_data(paper_json):
    """
    Convert the raw JSON data from Semantic Scholar into a structured dict.
    """
    if not paper_json:
        return None

    return {
        "title": paper_json.get("title") or "",
        "abstract": paper_json.get("abstract") or "",
        "authors": [a.get("name", "") for a in paper_json.get("authors", [])],
        "references": paper_json.get("references", []),
    }

def batch_fetch_papers(arxiv_ids, session, batch_size=50, max_attempts=5, base_sleep=1):
    """
    Fetch paper data for the given list of arXiv IDs in batches, with exponential backoff on HTTP 429.

    Args:
        arxiv_ids (list of str): The arXiv IDs to fetch.
        session (requests.Session): A requests session with retry logic.
        batch_size (int): Number of papers to request per API call.
        max_attempts (int): Maximum retry attempts if 429 or other recoverable errors occur.
        base_sleep (int): Base sleep time (in seconds) for exponential backoff.

    Returns:
        dict: Mapping from arxiv_id -> response JSON from Semantic Scholar.
              If a paper was not found or request failed, value may be None.
    """
    base_url = "https://api.semanticscholar.org/graph/v1/paper/batch"
    fields = "title,abstract,authors,references.title,references.abstract,references.authors"
    results = {}

    # Process the arXiv IDs in chunks
    for i in range(0, len(arxiv_ids), batch_size):
        chunk = arxiv_ids[i : i + batch_size]
        print(f"Processing chunk (size={len(chunk)}) starting at index {i}")

        payload = {
            "ids": [f"ARXIV:{arxiv_id}" for arxiv_id in chunk],
        }

        attempt = 1
        while attempt <= max_attempts:
            try:
                response = session.post(base_url, params= {"fields": fields}, json=payload, timeout=10)
                if response.status_code == 200:
                    papers_data = response.json()  # list of paper objects
                    # Each element corresponds to one ID in the same order
                    for idx, paper_json in enumerate(papers_data):
                        original_arxiv_id = chunk[idx]
                        results[original_arxiv_id] = paper_json
                    break  # Done with this chunk; proceed to next
                elif response.status_code == 429:
                    print(f"429 Too Many Requests. Attempt {attempt}/{max_attempts}")
                    # Optional: parse 'Retry-After' header if provided:
                    retry_after = response.headers.get("Retry-After")
                    if retry_after:
                        sleep_time = int(retry_after)
                    else:
                        # Exponential backoff: 1, 2, 4, 8...
                        sleep_time = base_sleep * 2 ** (attempt - 1)

                    print(f"Sleeping {sleep_time} seconds before retrying...")
                    time.sleep(sleep_time)
                    attempt += 1
                else:
                    # Some other non-200 error
                    print(f"Batch request failed (HTTP {response.status_code}) for chunk: {chunk}")
                    for arxiv_id in chunk:
                        results[arxiv_id] = None
                    break  # Stop retrying other status codes
            except requests.exceptions.RequestException as e:
                print(f"Network error on attempt {attempt}/{max_attempts} for chunk: {chunk}, error: {e}")
                # Could handle or retry on certain exceptions as well
                attempt += 1
                time.sleep(base_sleep * 2 ** (attempt - 1))

        else:
            # If we exit the while loop normally (no break), attempts were exhausted
            print(f"Max attempts reached. Marking this chunk as None.")
            for arxiv_id in chunk:
                if arxiv_id not in results:  # or if results[arxiv_id] hasn't been set
                    results[arxiv_id] = None

    return results


def fetch_all_papers_in_batches(arxiv_ids, batch_size=50):
    """
    Fetch data for all provided arXiv IDs using the batch endpoint.
    Uses caching to avoid redundant requests.

    Returns a dict {arxiv_id: processed_data}.
    """
    session = get_session()
    cache_data = load_cache()

    # Filter out those we haven't fetched yet
    arxiv_ids_to_fetch = [aid for aid in arxiv_ids if aid not in cache_data]

    if not arxiv_ids_to_fetch:
        print("All papers already in cache. Skipping fetch...")
        return cache_data

    print(f"Fetching {len(arxiv_ids_to_fetch)} papers in batches of {batch_size}...")

    # Fetch in batches
    batch_results = batch_fetch_papers(arxiv_ids_to_fetch, session, batch_size=batch_size)
    # Process & store results into cache
    for aid in arxiv_ids_to_fetch:
        paper_json = batch_results.get(aid)
        cache_data[aid] = process_paper_data(paper_json)

    # Save updated cache
    save_cache(cache_data)
    return cache_data


import requests
import xml.etree.ElementTree as ET
from datetime import datetime, timedelta

def fetch_arxiv_papers_with_dates(category="cs.IR", batch_size=100, max_papers=10000, start_date=None, end_date=None):
    """
    Fetches up to `max_papers` from the ArXiv API for a specific category using date ranges.

    Args:
        category (str): ArXiv category (e.g., cs.IR).
        batch_size (int): Number of papers to fetch per request.
        max_papers (int): Maximum number of papers to fetch in total.
        start_date (str): Start date in 'YYYY-MM-DD' format (default is 5 years ago).
        end_date (str): End date in 'YYYY-MM-DD' format (default is today).

    Returns:
        list of str: A list of ArXiv IDs.
    """
    url = "http://export.arxiv.org/api/query"
    fetched_papers = 0
    arxiv_ids = []

    # Default date range: Past 5 years to today
    if not end_date:
        end_date = datetime.utcnow().strftime("%Y-%m-%d")
    if not start_date:
        start_date = (datetime.utcnow() - timedelta(days=5 * 365)).strftime("%Y-%m-%d")

    start_date = datetime.strptime(start_date, "%Y-%m-%d")
    end_date = datetime.strptime(end_date, "%Y-%m-%d")

    current_start = start_date

    while current_start < end_date and fetched_papers < max_papers:
        current_end = current_start + timedelta(days=30)  # Fetch in 1-month chunks
        if current_end > end_date:
            current_end = end_date

        # Format the date range
        date_query = f"submittedDate:[{current_start.strftime('%Y%m%d')} TO {current_end.strftime('%Y%m%d')}]"

        start = 0
        while fetched_papers < max_papers:
            remaining = max_papers - fetched_papers
            if remaining <= 0:
                break
            current_batch_size = min(batch_size, remaining)

            params = {
                "search_query": f"cat:{category} AND {date_query}",
                "start": start,
                "max_results": current_batch_size,
                "sortBy": "submittedDate",
                "sortOrder": "descending"
            }

            response = requests.get(url, params=params)
            if response.status_code != 200:
                print(f"Error fetching papers: HTTP {response.status_code}")
                break

            root = ET.fromstring(response.content)
            ns = {"arxiv": "http://www.w3.org/2005/Atom"}
            entries = root.findall("arxiv:entry", ns)

            if not entries:
                break

            for entry in entries:
                arxiv_id = entry.find("arxiv:id", ns).text
                arxiv_id = arxiv_id.split('/')[-1].split('v')[0]
                arxiv_ids.append(arxiv_id)

            fetched_papers += len(entries)
            start += current_batch_size

        print(f"Fetched {fetched_papers} papers so far...")
        current_start = current_end + timedelta(days=1)

    print(f"Total papers fetched: {fetched_papers}")
    return arxiv_ids
