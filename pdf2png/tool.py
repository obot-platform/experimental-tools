import fitz
import os
import gptscript
import asyncio
import io

from PIL import Image


def convert_pdf_to_images(pdf_path, dpi=300) -> list[Image.Image]:
    """Converts each page of the PDF to an image."""
    doc = fitz.open(pdf_path)
    images = []
    for page_num in range(len(doc)):
        page = doc.load_page(page_num)
        zoom = dpi / 72
        mat = fitz.Matrix(zoom, zoom)
        pix = page.get_pixmap(matrix=mat)
        img = Image.frombytes("RGB", [pix.width, pix.height], pix.samples)
        images.append(img)
    return images


def prepend_base_path(base_path: str, file_path: str) -> str:
    """
    Prepend a base path to a file path if it's not already rooted in the base path.

    Args:
        base_path (str): The base path to prepend.
        file_path (str): The file path to check and modify.

    Returns:
        str: The modified file path with the base path prepended if necessary.

    Examples:
      >>> prepend_base_path("files", "my-file.txt")
      'files/my-file.txt'

      >>> prepend_base_path("files", "files/my-file.txt")
      'files/my-file.txt'

      >>> prepend_base_path("files", "foo/my-file.txt")
      'files/foo/my-file.txt'

      >>> prepend_base_path("files", "bar/files/my-file.txt")
      'files/bar/files/my-file.txt'

      >>> prepend_base_path("files", "files/bar/files/my-file.txt")
      'files/bar/files/my-file.txt'
    """
    # Split the file path into parts for checking
    file_parts = os.path.normpath(file_path).split(os.sep)

    # Check if the base path is already at the root
    if file_parts[0] == base_path:
        return file_path

    # Prepend the base path
    return os.path.join(base_path, file_path)


async def copy_pdf_from_gptscript_workspace(scratch_dir: str, filepath: str) -> str:
    gptscript_client = gptscript.GPTScript()
    wksp_file_path = prepend_base_path("files", filepath)
    file_content = await gptscript_client.read_file_in_workspace(wksp_file_path)

    # Save file to local workspace
    local_path = os.path.join(scratch_dir, os.path.basename(filepath))
    with open(local_path, "wb") as f:
        f.write(file_content)

    return local_path


async def save_to_gptscript_workspace(filepath: str, content: bytes | str) -> None:
    gptscript_client = gptscript.GPTScript()
    wksp_file_path = prepend_base_path("files", filepath)

    # Convert content to bytes if it's a string
    if isinstance(content, str):
        content = content.encode("utf-8")

    await gptscript_client.write_file_in_workspace(wksp_file_path, content)


def create_scratch_dir() -> str:
    gptscript_tool_dir = os.getenv("GPTSCRIPT_WORKSPACE_DIR")
    path = os.path.join(gptscript_tool_dir, "scratch")
    os.makedirs(path, exist_ok=True)
    return path


async def main():
    scratch_dir = create_scratch_dir()
    pdf_file_name = os.getenv("PDF_FILE")
    # Strip .pdf suffix if present
    base_name = pdf_file_name.rsplit(".pdf", 1)[0]
    local_pdf_file_path = await copy_pdf_from_gptscript_workspace(
        scratch_dir, pdf_file_name
    )

    images = convert_pdf_to_images(local_pdf_file_path)
    generated_files = []
    for i, img in enumerate(images):
        file_name = f"{base_name}_page_{i}.png"
        # Save image to bytes
        img_byte_arr = io.BytesIO()
        img.save(img_byte_arr, format="PNG")

        # Save to GPTScript workspace
        await save_to_gptscript_workspace(file_name, img_byte_arr.getvalue())
        generated_files.append(file_name)

    return {"images": generated_files}


if __name__ == "__main__":
    print(asyncio.run(main()))
