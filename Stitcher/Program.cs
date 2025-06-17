using System;
using System.IO;

class Program
{
    static void Main(string[] args)
    {
        if (args.Length > 0 && (args[0] == "-h" || args[0] == "--help"))
        {
            ShowHelp();
            return;
        }
        
        var inputDirectory = "in";
        var outputFile = DateTime.Now.ToString("s").Replace(":", "-");
        string? outputType = null;

        // Parse command-line args
        for (int i = 0; i < args.Length; i++)
        {
            switch (args[i])
            {
                case "-s":
                    if (i + 1 < args.Length)
                        inputDirectory = args[++i];
                    break;
                
                case "-o":
                    if (i + 1 < args.Length)
                        outputFile = args[++i];
                    break;
                
                case "-t":
                    if (i + 1 < args.Length)
                        outputType = args[++i];
                    break;
            }
        }

        inputDirectory = Path.GetFullPath(inputDirectory);
        if (!Directory.Exists(inputDirectory))
        {
            Console.WriteLine($"Directory not found: {inputDirectory}");
            return;
        }

        string[] files = Directory.GetFiles(inputDirectory);
        if (files.Length == 0)
        {
            Console.WriteLine($"No files found in: {inputDirectory}");
            return;
        }
        
        outputType ??= Path.GetExtension(files[0]);
        outputFile = Path.Combine(Directory.GetCurrentDirectory(), outputFile + outputType);

        using (var outputStream = new FileStream(outputFile, FileMode.Create, FileAccess.Write))
        {
            foreach (string filePath in files)
            {
                Console.WriteLine($"Merging: {filePath}");

                using var inputStream = new FileStream(filePath, FileMode.Open, FileAccess.Read);
                inputStream.CopyTo(outputStream);
            }
        }

        Console.WriteLine($"Done. Output written to: {outputFile}");
    }
    
    
    static void ShowHelp()
    {
        Console.WriteLine(@"
Stitcher
-----------------
Usage:
  stitcher.exe [-s <source_folder>] [-o <output_name>] [-t <output_extension>] [-h]

Options:
  -s, <folder>      Source directory to read input files from (default: 'in')
  -o, <name>        Output file name (without extension). Default is a timestamp
  -t, <extension>   Output file extension (e.g., txt, bin). Defaults to first input file's extension
  -h, --help        Show this help message and exit

Examples:
  stitcher.exe
      Merges files from ./in into something like 2025-05-22T19-42-00.txt

  stitcher.exe -s input -o merged -t csv
      Merges files from ./input into merged.csv
");
    }
    
}